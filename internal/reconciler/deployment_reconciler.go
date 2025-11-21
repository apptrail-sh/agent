package reconciler

import (
	"context"

	v1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/apptrail-sh/controller/internal/model"
)

// DeploymentReconciler reconciles Deployment objects
type DeploymentReconciler struct {
	*WorkloadReconciler
}

func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string) *DeploymentReconciler {
	return &DeploymentReconciler{
		WorkloadReconciler: NewWorkloadReconciler(client, scheme, recorder, publisherChan, controllerNamespace),
	}
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=apptrail.apptrail.sh,resources=workloadrolloutstates,verbs=get;list;watch;create;update;patch;delete

func (dr *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling Deployment")

	resource := &v1.Deployment{}
	if err := dr.Get(ctx, req.NamespacedName, resource); err != nil {
		if apierrors.IsNotFound(err) {
			// Deployment was deleted, clean up state
			_ = dr.HandleDeletion(ctx, req.Namespace, req.Name, "Deployment")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Deployment found", "Deployment", resource)

	// Wrap the Deployment in an adapter
	adapter := &DeploymentAdapter{Deployment: resource}

	// Use the shared reconciliation logic
	return dr.ReconcileWorkload(ctx, req, adapter)
}

// SetupWithManager sets up the controller with the Manager.
func (dr *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Deployment{}).
		Complete(dr)
}
