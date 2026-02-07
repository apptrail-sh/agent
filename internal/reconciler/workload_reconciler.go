package reconciler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	apptrailv1alpha1 "github.com/apptrail-sh/agent/api/v1alpha1"
	"github.com/apptrail-sh/agent/internal/filter"
	"github.com/apptrail-sh/agent/internal/model"

	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	appVersionMetricName = "apptrail_app_version"

	// Workload phases
	phaseRollingOut  = "rolling_out"
	phaseFailed      = "failed"
	phaseSuccess     = "success"
	phaseProgressing = "progressing"
)

var (
	appVersionGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: appVersionMetricName,
		Help: "App version for a given workload (Deployment, StatefulSet, DaemonSet)",
	}, []string{
		"namespace",
		"workload",
		"kind",
		"previous_version",
		"current_version",
		"last_updated",
	})

	metricsRegistered = false
)

type AppVersion struct {
	PreviousVersion string
	CurrentVersion  string
	LastUpdated     time.Time
	RolloutStarted  time.Time // When rollout started
}

// WorkloadReconciler contains shared logic for reconciling workloads
type WorkloadReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	mu                  sync.RWMutex // Protects workloadVersions and workloadPhases
	workloadVersions    map[string]AppVersion
	workloadPhases      map[string]string // Track last sent phase
	publisherChan       chan<- model.WorkloadUpdate
	controllerNamespace string // Namespace where controller is running
	filter              *filter.ResourceFilter
}

func NewWorkloadReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string, resourceFilter *filter.ResourceFilter) *WorkloadReconciler {
	// Register metrics only once
	if !metricsRegistered {
		metrics.Registry.MustRegister(appVersionGauge)
		metricsRegistered = true
	}

	return &WorkloadReconciler{
		Client:              client,
		Scheme:              scheme,
		Recorder:            recorder,
		workloadVersions:    make(map[string]AppVersion),
		workloadPhases:      make(map[string]string),
		publisherChan:       publisherChan,
		controllerNamespace: controllerNamespace,
		filter:              resourceFilter,
	}
}

// ReconcileWorkload contains the shared reconciliation logic for all workload types
func (wr *WorkloadReconciler) ReconcileWorkload(ctx context.Context, req ctrl.Request, workload WorkloadAdapter) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Skip workloads in excluded namespaces
	if wr.filter != nil && !wr.filter.ShouldWatchNamespace(req.Namespace) {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling workload", "kind", workload.GetKind(), "name", workload.GetName())

	appkey := workload.GetNamespace() + "/" + workload.GetName() + "/" + workload.GetKind()

	// Read stored state under read lock
	wr.mu.RLock()
	stored := wr.workloadVersions[appkey]
	lastPhase := wr.workloadPhases[appkey]
	wr.mu.RUnlock()

	versionLabel := workload.GetVersion()
	if versionLabel == "" {
		log.Info("Workload version label not found",
			"kind", workload.GetKind(),
			"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()))
		return ctrl.Result{}, nil
	}

	// Load persistent state from CRD if in-memory state is empty (e.g., after restart)
	var crdState RolloutState
	if stored.RolloutStarted.IsZero() {
		var err error
		crdState, err = wr.loadFullRolloutStateFromCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind())
		if err != nil {
			log.Error(err, "Failed to load rollout state from CRD")
			// Continue with in-memory state
		} else {
			if !crdState.RolloutStarted.IsZero() {
				stored.RolloutStarted = crdState.RolloutStarted
				// Update in-memory map so determineWorkloadPhase can access it
				wr.mu.Lock()
				wr.workloadVersions[appkey] = stored
				wr.mu.Unlock()
				log.Info("Loaded rollout state from CRD", "rolloutStarted", crdState.RolloutStarted)
			}
			// Also restore phase tracking from CRD if we have it
			if crdState.LastSentPhase != "" && lastPhase == "" {
				wr.mu.Lock()
				wr.workloadPhases[appkey] = crdState.LastSentPhase
				wr.mu.Unlock()
				lastPhase = crdState.LastSentPhase
				log.Info("Restored phase tracking from CRD", "lastSentPhase", crdState.LastSentPhase)
			}
		}
	}

	// Determine current workload phase
	currentPhase := wr.determineWorkloadPhase(workload, appkey)

	// Send event if version changed OR phase changed
	versionChanged := stored.CurrentVersion != versionLabel
	phaseChanged := lastPhase != currentPhase

	// Check for restart deduplication: if we have CRD state, verify this is a real change
	// not just a re-reconciliation of the same state after restart
	if crdState.LastSentVersion != "" {
		// We loaded state from CRD, check if current state matches what we last sent
		if crdState.LastSentVersion == versionLabel && crdState.LastSentPhase == currentPhase {
			log.Info("Skipping duplicate event after restart",
				"workload", appkey,
				"version", versionLabel,
				"phase", currentPhase)

			// Refresh metrics from current state (decoupled from event publishing)
			previousVersion := stored.PreviousVersion
			if previousVersion == "" {
				previousVersion = crdState.LastSentVersion
			}
			wr.refreshWorkloadMetrics(workload, previousVersion, versionLabel)

			// Update in-memory state to match CRD (so subsequent changes will be detected)
			wr.mu.Lock()
			if stored.CurrentVersion != versionLabel {
				stored.CurrentVersion = versionLabel
				stored.PreviousVersion = crdState.LastSentVersion
				wr.workloadVersions[appkey] = stored
			}
			wr.workloadPhases[appkey] = currentPhase
			wr.mu.Unlock()

			if currentPhase == phaseRollingOut {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	// Track rollout timing
	// Set RolloutStarted when entering rolling_out phase (or on version change)
	// Clear it when leaving rolling_out phase
	needsPersistence := false
	if currentPhase == phaseRollingOut && stored.RolloutStarted.IsZero() {
		// Entering rolling_out phase for the first time
		stored.RolloutStarted = time.Now()
		needsPersistence = true
		log.Info("Rollout started", "workload", appkey, "time", stored.RolloutStarted)
	} else if currentPhase != phaseRollingOut && !stored.RolloutStarted.IsZero() {
		// Left rolling_out phase, clear the in-memory timer
		// Keep CRD for dedup and metrics refresh on restart
		stored.RolloutStarted = time.Time{}
		log.Info("Rollout completed", "workload", appkey)
	}

	if versionChanged || phaseChanged {
		// Update version tracking if version changed
		if versionChanged {
			newAppVer := AppVersion{
				PreviousVersion: stored.CurrentVersion,
				CurrentVersion:  versionLabel,
				LastUpdated:     time.Now(),
				RolloutStarted:  stored.RolloutStarted, // Preserve rollout timer
			}
			wr.mu.Lock()
			wr.workloadVersions[appkey] = newAppVer
			wr.mu.Unlock()
			stored = newAppVer // Update local reference

			wr.refreshWorkloadMetrics(workload, stored.PreviousVersion, versionLabel)
			log.Info("Updated workload version metric", "workload", workload.GetName(), "kind", workload.GetKind())
		} else {
			// Version didn't change but we might have updated RolloutStarted
			wr.mu.Lock()
			wr.workloadVersions[appkey] = stored
			wr.mu.Unlock()
		}

		// Update phase tracking
		wr.mu.Lock()
		wr.workloadPhases[appkey] = currentPhase
		wr.mu.Unlock()

		// Persist state to CRD for deduplication after restart
		// Always persist when we send an event, not just when rollout starts
		err := wr.saveFullRolloutStateToCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind(), versionLabel, stored.RolloutStarted, versionLabel, currentPhase)
		if err != nil {
			log.Error(err, "Failed to persist rollout state to CRD")
			// Don't fail the reconciliation, continue with in-memory state
		}

		// Send event with current state
		wr.publisherChan <- model.WorkloadUpdate{
			Name:            workload.GetName(),
			Namespace:       workload.GetNamespace(),
			Kind:            workload.GetKind(),
			PreviousVersion: stored.PreviousVersion,
			CurrentVersion:  versionLabel,
			Labels:          workload.GetLabels(),

			// Workload status
			DeploymentPhase: currentPhase,
		}

		if versionChanged {
			log.Info("Workload version updated",
				"kind", workload.GetKind(),
				"workload", workload.GetName(),
				"phase", currentPhase)
		} else {
			log.Info("Workload phase updated",
				"kind", workload.GetKind(),
				"workload", workload.GetName(),
				"previousPhase", lastPhase,
				"currentPhase", currentPhase)
		}
	} else if needsPersistence {
		// Even if no event to send, persist rollout start time if needed
		err := wr.saveFullRolloutStateToCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind(), versionLabel, stored.RolloutStarted, versionLabel, currentPhase)
		if err != nil {
			log.Error(err, "Failed to persist rollout state to CRD")
		}
	}

	// If workload is rolling out, requeue to check timeout periodically
	if currentPhase == phaseRollingOut {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// refreshWorkloadMetrics updates the Prometheus gauge for a workload.
// Called to ensure metrics reflect current state regardless of event publishing.
func (wr *WorkloadReconciler) refreshWorkloadMetrics(workload WorkloadAdapter, previousVersion, currentVersion string) {
	labelsToDelete := map[string]string{
		"namespace": workload.GetNamespace(),
		"workload":  workload.GetName(),
		"kind":      workload.GetKind(),
	}
	appVersionGauge.DeletePartialMatch(labelsToDelete)

	appVersionGauge.WithLabelValues(
		workload.GetNamespace(),
		workload.GetName(),
		workload.GetKind(),
		previousVersion,
		currentVersion,
		time.Now().Format(time.RFC3339),
	).Set(1)
}

// determineWorkloadPhase determines the workload phase based on Kubernetes status
func (wr *WorkloadReconciler) determineWorkloadPhase(workload WorkloadAdapter, appkey string) string {
	// Check replica status to determine if rolling out
	isRollingOut := workload.IsRollingOut()

	// Check for explicit failure conditions from Kubernetes
	if workload.HasFailed() {
		return phaseFailed
	}

	// If rolling out, check timeout BEFORE returning rolling_out status
	if isRollingOut {
		// Additional check: Has rollout been in progress too long?
		// This catches cases where Flux/ArgoCD resets the K8s progress deadline
		wr.mu.RLock()
		stored := wr.workloadVersions[appkey]
		wr.mu.RUnlock()
		if !stored.RolloutStarted.IsZero() {
			elapsed := time.Since(stored.RolloutStarted)
			// Force failed after 15 minutes (longer than K8s default to account for resets)
			if elapsed > 15*time.Minute {
				return phaseFailed
			}
		}
		return phaseRollingOut
	}

	// All replicas ready and updated
	if workload.GetReadyReplicas() == workload.GetTotalReplicas() &&
		workload.GetUpdatedReplicas() == workload.GetTotalReplicas() {
		return phaseSuccess
	}

	return phaseProgressing
}

// RolloutState contains the state loaded from the CRD
type RolloutState struct {
	RolloutStarted  time.Time
	LastSentVersion string
	LastSentPhase   string
	LastSentAt      time.Time
}

// loadFullRolloutStateFromCRD loads the complete rollout state from the CRD including deduplication fields
func (wr *WorkloadReconciler) loadFullRolloutStateFromCRD(ctx context.Context, namespace, name, kind string) (RolloutState, error) {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, strings.ToLower(kind))
	state := &apptrailv1alpha1.WorkloadRolloutState{}

	err := wr.Get(ctx, types.NamespacedName{
		Name:      stateName,
		Namespace: wr.controllerNamespace,
	}, state)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return RolloutState{}, nil // No state stored yet
		}
		log.Error(err, "Failed to load rollout state", "stateName", stateName)
		return RolloutState{}, err
	}

	result := RolloutState{
		RolloutStarted:  state.Spec.RolloutStarted.Time,
		LastSentVersion: state.Spec.LastSentVersion,
		LastSentPhase:   state.Spec.LastSentPhase,
	}
	if state.Spec.LastSentAt != nil {
		result.LastSentAt = state.Spec.LastSentAt.Time
	}

	return result, nil
}

// saveFullRolloutStateToCRD saves the complete rollout state to a CRD including deduplication fields
func (wr *WorkloadReconciler) saveFullRolloutStateToCRD(ctx context.Context, namespace, name, kind, version string, rolloutStarted time.Time, lastSentVersion, lastSentPhase string) error {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, strings.ToLower(kind))
	now := metav1.Now()
	state := &apptrailv1alpha1.WorkloadRolloutState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateName,
			Namespace: wr.controllerNamespace,
		},
		Spec: apptrailv1alpha1.WorkloadRolloutStateSpec{
			WorkloadNamespace: namespace,
			WorkloadName:      name,
			WorkloadKind:      kind,
			Version:           version,
			RolloutStarted:    metav1.Time{Time: rolloutStarted},
			LastSentVersion:   lastSentVersion,
			LastSentPhase:     lastSentPhase,
			LastSentAt:        &now,
		},
	}

	// Try to create, if it exists, update it
	err := wr.Create(ctx, state)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Update existing state
			existingState := &apptrailv1alpha1.WorkloadRolloutState{}
			err = wr.Get(ctx, types.NamespacedName{
				Name:      stateName,
				Namespace: wr.controllerNamespace,
			}, existingState)
			if err != nil {
				return err
			}

			existingState.Spec = state.Spec
			err = wr.Update(ctx, existingState)
			if err != nil {
				log.Error(err, "Failed to update rollout state", "stateName", stateName)
				return err
			}
		} else {
			log.Error(err, "Failed to create rollout state", "stateName", stateName)
			return err
		}
	}

	return nil
}

// deleteRolloutStateFromCRD deletes the rollout state CRD
func (wr *WorkloadReconciler) deleteRolloutStateFromCRD(ctx context.Context, namespace, name, kind string) error {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, strings.ToLower(kind))
	state := &apptrailv1alpha1.WorkloadRolloutState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateName,
			Namespace: wr.controllerNamespace,
		},
	}

	err := wr.Delete(ctx, state)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to delete rollout state", "stateName", stateName)
		return err
	}

	return nil
}

// HandleDeletion handles cleanup when a workload is deleted
func (wr *WorkloadReconciler) HandleDeletion(ctx context.Context, namespace, name, kind string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Workload deleted, cleaning up state", "kind", kind, "namespace", namespace, "name", name)
	return wr.deleteRolloutStateFromCRD(ctx, namespace, name, kind)
}
