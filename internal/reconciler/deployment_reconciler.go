package reconciler

import (
	"context"
	"time"

	v1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/apptrail-sh/agent/internal/model"
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
		WithEventFilter(DeploymentStatusChangedPredicate()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
				200*time.Millisecond,
				10*time.Minute,
			),
		}).
		Complete(dr)
}
