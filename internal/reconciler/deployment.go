package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/apptrail-sh/controller/internal/model"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	appVersionMetricName = "apptrail_app_version"
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
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	deploymentVersions map[string]AppVersion
	deploymentPhases   map[string]string // Track last sent phase
	publisherChan      chan<- model.WorkloadUpdate
}

func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate) *DeploymentReconciler {
	metrics.Registry.MustRegister(appVersionGauge)
	return &DeploymentReconciler{
		Client:             client,
		Scheme:             scheme,
		Recorder:           recorder,
		deploymentVersions: make(map[string]AppVersion),
		deploymentPhases:   make(map[string]string),
		publisherChan:      publisherChan,
	}
}

func (dr *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling Deployment")

	resource := &v1.Deployment{}
	if err := dr.Get(ctx, req.NamespacedName, resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	// Determine current deployment phase
	currentPhase := dr.determineDeploymentPhase(resource, appkey)
	lastPhase := dr.deploymentPhases[appkey]

	// Send event if version changed OR phase changed
	versionChanged := stored.CurrentVersion != versionLabel
	phaseChanged := lastPhase != currentPhase

	if versionChanged || phaseChanged {
		// Update version tracking if version changed
		if versionChanged {
			newAppVer := AppVersion{
				PreviousVersion: stored.CurrentVersion,
				CurrentVersion:  versionLabel,
				LastUpdated:     time.Now(),
				RolloutStarted:  time.Now(), // Track when rollout started
			}
			dr.deploymentVersions[appkey] = newAppVer

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
		}

		// Update phase tracking
		dr.deploymentPhases[appkey] = currentPhase

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

	return ctrl.Result{}, nil
}

// determineDeploymentPhase determines the deployment phase based on Kubernetes status
func (dr *DeploymentReconciler) determineDeploymentPhase(deployment *v1.Deployment, appkey string) string {
	// Check deployment conditions first
	for _, condition := range deployment.Status.Conditions {
		switch condition.Type {
		case v1.DeploymentProgressing:
			if condition.Status == "False" {
				return "failed"
			}
			if condition.Reason == "ProgressDeadlineExceeded" {
				return "failed"
			}
		case v1.DeploymentAvailable:
			if condition.Status == "False" {
				return "rolling_out"
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
				return "failed"
			}
		}
		return "rolling_out"
	}

	// All replicas ready and updated
	if deployment.Status.ReadyReplicas == deployment.Status.Replicas &&
		deployment.Status.UpdatedReplicas == deployment.Status.Replicas {
		return "success"
	}

	return "progressing"
}

// SetupWithManager sets up the controller with the Manager.
func (dr *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Deployment{}).
		Complete(dr)
}
