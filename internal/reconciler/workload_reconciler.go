package reconciler

import (
	"context"
	"fmt"
	"time"

	apptrailv1alpha1 "github.com/apptrail-sh/agent/api/v1alpha1"
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
	workloadVersions    map[string]AppVersion
	workloadPhases      map[string]string // Track last sent phase
	publisherChan       chan<- model.WorkloadUpdate
	controllerNamespace string // Namespace where controller is running
}

func NewWorkloadReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string) *WorkloadReconciler {
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
	}
}

// ReconcileWorkload contains the shared reconciliation logic for all workload types
func (wr *WorkloadReconciler) ReconcileWorkload(ctx context.Context, req ctrl.Request, workload WorkloadAdapter) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling workload", "kind", workload.GetKind(), "name", workload.GetName())

	appkey := workload.GetNamespace() + "/" + workload.GetName() + "/" + workload.GetKind()
	stored := wr.workloadVersions[appkey]

	versionLabel := workload.GetVersion()
	if versionLabel == "" {
		log.Info("Workload version label not found",
			"kind", workload.GetKind(),
			"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()))
		return ctrl.Result{}, nil
	}

	// Load persistent state from CRD if in-memory state is empty
	if stored.RolloutStarted.IsZero() {
		crdRolloutStarted, err := wr.loadRolloutStateFromCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind())
		if err != nil {
			log.Error(err, "Failed to load rollout state from CRD")
			// Continue with in-memory state
		} else if !crdRolloutStarted.IsZero() {
			stored.RolloutStarted = crdRolloutStarted
			// Update in-memory map so determineWorkloadPhase can access it
			wr.workloadVersions[appkey] = stored
			log.Info("Loaded rollout state from CRD", "rolloutStarted", crdRolloutStarted)
		}
	}

	// Determine current workload phase
	currentPhase := wr.determineWorkloadPhase(workload, appkey)
	lastPhase := wr.workloadPhases[appkey]

	// Send event if version changed OR phase changed
	versionChanged := stored.CurrentVersion != versionLabel
	phaseChanged := lastPhase != currentPhase

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
		// Left rolling_out phase, clear the timer and delete CRD
		stored.RolloutStarted = time.Time{}
		log.Info("Rollout completed, cleaning up state", "workload", appkey)
		_ = wr.deleteRolloutStateFromCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind())
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
			wr.workloadVersions[appkey] = newAppVer
			stored = newAppVer // Update local reference

			timeFormatted := newAppVer.LastUpdated.Format(time.RFC3339)

			labelsToDelete := make(map[string]string)
			labelsToDelete["namespace"] = workload.GetNamespace()
			labelsToDelete["workload"] = workload.GetName()
			labelsToDelete["kind"] = workload.GetKind()

			deleted := appVersionGauge.DeletePartialMatch(labelsToDelete)
			if deleted > 0 {
				log.Info("Deleted old workload version metric", "workload", workload.GetName(), "kind", workload.GetKind())
			}

			appVersionGauge.WithLabelValues(
				workload.GetNamespace(),
				workload.GetName(),
				workload.GetKind(),
				stored.CurrentVersion,
				versionLabel,
				timeFormatted).Set(1)
		} else {
			// Version didn't change but we might have updated RolloutStarted
			wr.workloadVersions[appkey] = stored
		}

		// Update phase tracking
		wr.workloadPhases[appkey] = currentPhase

		// Persist rollout state to CRD if needed
		if needsPersistence && !stored.RolloutStarted.IsZero() {
			err := wr.saveRolloutStateToCRD(ctx, workload.GetNamespace(), workload.GetName(), workload.GetKind(), versionLabel, stored.RolloutStarted)
			if err != nil {
				log.Error(err, "Failed to persist rollout state to CRD")
				// Don't fail the reconciliation, continue with in-memory state
			}
		}

		// Send event with current state
		wr.publisherChan <- model.WorkloadUpdate{
			Name:            workload.GetName(),
			Namespace:       workload.GetNamespace(),
			Kind:            workload.GetKind(),
			PreviousVersion: stored.CurrentVersion,
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
	}

	// If workload is rolling out, requeue to check timeout periodically
	if currentPhase == phaseRollingOut {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
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
		stored := wr.workloadVersions[appkey]
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

// loadRolloutStateFromCRD loads the rollout state from the CRD if it exists
func (wr *WorkloadReconciler) loadRolloutStateFromCRD(ctx context.Context, namespace, name, kind string) (time.Time, error) {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, kind)
	state := &apptrailv1alpha1.WorkloadRolloutState{}

	err := wr.Get(ctx, types.NamespacedName{
		Name:      stateName,
		Namespace: wr.controllerNamespace,
	}, state)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return time.Time{}, nil // No state stored yet
		}
		log.Error(err, "Failed to load rollout state", "stateName", stateName)
		return time.Time{}, err
	}

	return state.Spec.RolloutStarted.Time, nil
}

// saveRolloutStateToCRD saves the rollout state to a CRD
func (wr *WorkloadReconciler) saveRolloutStateToCRD(ctx context.Context, namespace, name, kind, version string, rolloutStarted time.Time) error {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, kind)
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

	stateName := fmt.Sprintf("%s-%s-%s", namespace, name, kind)
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
