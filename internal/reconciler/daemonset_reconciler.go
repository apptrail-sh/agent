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

	"github.com/apptrail-sh/agent/internal/filter"
	"github.com/apptrail-sh/agent/internal/model"
)

// DaemonSetReconciler reconciles DaemonSet objects
type DaemonSetReconciler struct {
	*WorkloadReconciler
}

func NewDaemonSetReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string, resourceFilter *filter.ResourceFilter) *DaemonSetReconciler {
	return &DaemonSetReconciler{
		WorkloadReconciler: NewWorkloadReconciler(client, scheme, recorder, publisherChan, controllerNamespace, resourceFilter),
	}
}

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets/status,verbs=get
// +kubebuilder:rbac:groups=apptrail.apptrail.sh,resources=workloadrolloutstates,verbs=get;list;watch;create;update;patch;delete

func (dsr *DaemonSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling DaemonSet")

	resource := &v1.DaemonSet{}
	if err := dsr.Get(ctx, req.NamespacedName, resource); err != nil {
		if apierrors.IsNotFound(err) {
			// DaemonSet was deleted, clean up state
			_ = dsr.HandleDeletion(ctx, req.Namespace, req.Name, "DaemonSet")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("DaemonSet found", "DaemonSet", resource)

	// Wrap the DaemonSet in an adapter
	adapter := &DaemonSetAdapter{DaemonSet: resource}

	// Use the shared reconciliation logic
	return dsr.ReconcileWorkload(ctx, req, adapter)
}

// SetupWithManager sets up the controller with the Manager.
func (dsr *DaemonSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.DaemonSet{}).
		WithEventFilter(DaemonSetStatusChangedPredicate()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
				200*time.Millisecond,
				10*time.Minute,
			),
		}).
		Complete(dsr)
}
