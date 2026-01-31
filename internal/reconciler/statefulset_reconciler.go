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

// StatefulSetReconciler reconciles StatefulSet objects
type StatefulSetReconciler struct {
	*WorkloadReconciler
}

func NewStatefulSetReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, publisherChan chan<- model.WorkloadUpdate, controllerNamespace string) *StatefulSetReconciler {
	return &StatefulSetReconciler{
		WorkloadReconciler: NewWorkloadReconciler(client, scheme, recorder, publisherChan, controllerNamespace),
	}
}

// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get
// +kubebuilder:rbac:groups=apptrail.apptrail.sh,resources=workloadrolloutstates,verbs=get;list;watch;create;update;patch;delete

func (sr *StatefulSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling StatefulSet")

	resource := &v1.StatefulSet{}
	if err := sr.Get(ctx, req.NamespacedName, resource); err != nil {
		if apierrors.IsNotFound(err) {
			// StatefulSet was deleted, clean up state
			_ = sr.HandleDeletion(ctx, req.Namespace, req.Name, "StatefulSet")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("StatefulSet found", "StatefulSet", resource)

	// Wrap the StatefulSet in an adapter
	adapter := &StatefulSetAdapter{StatefulSet: resource}

	// Use the shared reconciliation logic
	return sr.ReconcileWorkload(ctx, req, adapter)
}

// SetupWithManager sets up the controller with the Manager.
func (sr *StatefulSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.StatefulSet{}).
		WithEventFilter(StatefulSetStatusChangedPredicate()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
				200*time.Millisecond,
				10*time.Minute,
			),
		}).
		Complete(sr)
}
