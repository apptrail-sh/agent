package reconciler

import (
	"context"
	"fmt"
	"time"

	apptrailv1alpha1 "github.com/apptrail-sh/controller/api/v1alpha1"
	"github.com/apptrail-sh/controller/internal/model"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/apps/v1"
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

	// Deployment phases
	phaseRollingOut  = "rolling_out"
	phaseFailed      = "failed"
	phaseSuccess     = "success"
	phaseProgressing = "progressing"
)

var (
	appVersionGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: appVersionMetricName,
		Help: "App version for a given deployment",
	}, []string{
		"namespace",
		"app",
		"previous_version",
		"current_version",
		"last_updated",
	})
)

type AppVersion struct {
	PreviousVersion string
	CurrentVersion  string
	LastUpdated     time.Time
	RolloutStarted  time.Time // When rollout started
}

type DeploymentReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	deploymentVersions  map[string]AppVersion
	deploymentPhases    map[string]string // Track last sent phase
	publisherChan       chan<- model.WorkloadUpdate
	controllerNamespace string // Namespace where controller is running
}

func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string) *DeploymentReconciler {
	metrics.Registry.MustRegister(appVersionGauge)
	return &DeploymentReconciler{
		Client:              client,
		Scheme:              scheme,
		Recorder:            recorder,
		deploymentVersions:  make(map[string]AppVersion),
		deploymentPhases:    make(map[string]string),
		publisherChan:       publisherChan,
		controllerNamespace: controllerNamespace,
	}
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=apptrail.apptrail.sh,resources=deploymentrolloutstates,verbs=get;list;watch;create;update;patch;delete

func (dr *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling Deployment")

	resource := &v1.Deployment{}
	if err := dr.Get(ctx, req.NamespacedName, resource); err != nil {
		if apierrors.IsNotFound(err) {
			// Deployment was deleted, clean up state
			log.Info("Deployment deleted, cleaning up state", "namespace", req.Namespace, "name", req.Name)
			_ = dr.deleteRolloutStateFromCRD(ctx, req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Deployment found", "Deployment", resource)

	appkey := req.Namespace + "/" + req.Name
	stored := dr.deploymentVersions[appkey]

	versionLabel := resource.Labels["app.kubernetes.io/version"]
	if versionLabel == "" {
		log.Info("Deployment version label not found",
			"Deployment", fmt.Sprintf("%s/%s", req.Namespace, req.Name))
		return ctrl.Result{}, nil
	}

	// Load persistent state from CRD if in-memory state is empty
	if stored.RolloutStarted.IsZero() {
		crdRolloutStarted, err := dr.loadRolloutStateFromCRD(ctx, req.Namespace, req.Name)
		if err != nil {
			log.Error(err, "Failed to load rollout state from CRD")
			// Continue with in-memory state
		} else if !crdRolloutStarted.IsZero() {
			stored.RolloutStarted = crdRolloutStarted
			log.Info("Loaded rollout state from CRD", "rolloutStarted", crdRolloutStarted)
		}
	}

	// Determine current deployment phase
	currentPhase := dr.determineDeploymentPhase(resource, appkey)
	lastPhase := dr.deploymentPhases[appkey]

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
		log.Info("Rollout started", "deployment", appkey, "time", stored.RolloutStarted)
	} else if currentPhase != phaseRollingOut && !stored.RolloutStarted.IsZero() {
		// Left rolling_out phase, clear the timer and delete CRD
		stored.RolloutStarted = time.Time{}
		log.Info("Rollout completed, cleaning up state", "deployment", appkey)
		_ = dr.deleteRolloutStateFromCRD(ctx, req.Namespace, req.Name)
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
			dr.deploymentVersions[appkey] = newAppVer
			stored = newAppVer // Update local reference

			timeFormatted := newAppVer.LastUpdated.Format(time.RFC3339)

			labelsToDelete := make(map[string]string)
			labelsToDelete["namespace"] = resource.Namespace
			labelsToDelete["app"] = resource.Name

			deleted := appVersionGauge.DeletePartialMatch(labelsToDelete)
			if deleted > 0 {
				log.Info("Deleted old deployment version metric", "Deployment", resource)
			}

			appVersionGauge.WithLabelValues(
				resource.Namespace,
				resource.Name,
				stored.CurrentVersion,
				versionLabel,
				timeFormatted).Set(1)
		} else {
			// Version didn't change but we might have updated RolloutStarted
			dr.deploymentVersions[appkey] = stored
		}

		// Update phase tracking
		dr.deploymentPhases[appkey] = currentPhase

		// Persist rollout state to CRD if needed
		if needsPersistence && !stored.RolloutStarted.IsZero() {
			err := dr.saveRolloutStateToCRD(ctx, req.Namespace, req.Name, versionLabel, stored.RolloutStarted)
			if err != nil {
				log.Error(err, "Failed to persist rollout state to CRD")
				// Don't fail the reconciliation, continue with in-memory state
			}
		}

		// Send event with current state
		dr.publisherChan <- model.WorkloadUpdate{
			Name:            resource.Name,
			Namespace:       resource.Namespace,
			Kind:            resource.Kind,
			PreviousVersion: stored.CurrentVersion,
			CurrentVersion:  versionLabel,
			Labels:          resource.Labels,

			// Deployment status
			DeploymentPhase:   currentPhase,
			ReplicasTotal:     resource.Status.Replicas,
			ReplicasReady:     resource.Status.ReadyReplicas,
			ReplicasUpdated:   resource.Status.UpdatedReplicas,
			ReplicasAvailable: resource.Status.AvailableReplicas,
		}

		if versionChanged {
			log.Info("Deployment version updated",
				"Deployment", resource,
				"phase", currentPhase,
				"replicas", fmt.Sprintf("%d/%d ready", resource.Status.ReadyReplicas, resource.Status.Replicas))
		} else {
			log.Info("Deployment phase updated",
				"Deployment", resource,
				"previousPhase", lastPhase,
				"currentPhase", currentPhase,
				"replicas", fmt.Sprintf("%d/%d ready", resource.Status.ReadyReplicas, resource.Status.Replicas))
		}
	}

	// If deployment is rolling out, requeue to check timeout periodically
	if currentPhase == phaseRollingOut {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// determineDeploymentPhase determines the deployment phase based on Kubernetes status
func (dr *DeploymentReconciler) determineDeploymentPhase(deployment *v1.Deployment, appkey string) string {
	// Check deployment conditions first
	for _, condition := range deployment.Status.Conditions {
		switch condition.Type {
		case v1.DeploymentProgressing:
			if condition.Status == "False" {
				return phaseFailed
			}
			if condition.Reason == "ProgressDeadlineExceeded" {
				return phaseFailed
			}
		case v1.DeploymentAvailable:
			if condition.Status == "False" {
				return phaseRollingOut
			}
		}
	}

	// Check replica status
	isRollingOut := deployment.Status.UpdatedReplicas < deployment.Status.Replicas ||
		deployment.Status.ReadyReplicas < deployment.Status.Replicas

	if isRollingOut {
		// Additional check: Has rollout been in progress too long?
		// This catches cases where Flux/ArgoCD resets the K8s progress deadline
		stored := dr.deploymentVersions[appkey]
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
	if deployment.Status.ReadyReplicas == deployment.Status.Replicas &&
		deployment.Status.UpdatedReplicas == deployment.Status.Replicas {
		return phaseSuccess
	}

	return phaseProgressing
}

// loadRolloutStateFromCRD loads the rollout state from the CRD if it exists
func (dr *DeploymentReconciler) loadRolloutStateFromCRD(ctx context.Context, namespace, name string) (time.Time, error) {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s", namespace, name)
	state := &apptrailv1alpha1.DeploymentRolloutState{}

	err := dr.Get(ctx, types.NamespacedName{
		Name:      stateName,
		Namespace: dr.controllerNamespace,
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
func (dr *DeploymentReconciler) saveRolloutStateToCRD(ctx context.Context, namespace, name, version string, rolloutStarted time.Time) error {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s", namespace, name)
	state := &apptrailv1alpha1.DeploymentRolloutState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateName,
			Namespace: dr.controllerNamespace,
		},
		Spec: apptrailv1alpha1.DeploymentRolloutStateSpec{
			DeploymentNamespace: namespace,
			DeploymentName:      name,
			Version:             version,
			RolloutStarted:      metav1.Time{Time: rolloutStarted},
		},
	}

	// Try to create, if it exists, update it
	err := dr.Create(ctx, state)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Update existing state
			existingState := &apptrailv1alpha1.DeploymentRolloutState{}
			err = dr.Get(ctx, types.NamespacedName{
				Name:      stateName,
				Namespace: dr.controllerNamespace,
			}, existingState)
			if err != nil {
				return err
			}

			existingState.Spec = state.Spec
			err = dr.Update(ctx, existingState)
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
func (dr *DeploymentReconciler) deleteRolloutStateFromCRD(ctx context.Context, namespace, name string) error {
	log := ctrl.LoggerFrom(ctx)

	stateName := fmt.Sprintf("%s-%s", namespace, name)
	state := &apptrailv1alpha1.DeploymentRolloutState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateName,
			Namespace: dr.controllerNamespace,
		},
	}

	err := dr.Delete(ctx, state)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to delete rollout state", "stateName", stateName)
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (dr *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Deployment{}).
		Complete(dr)
}
