package infrastructure

import (
	"context"

	"github.com/apptrail-sh/agent/internal/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodReconciler reconciles Pod objects
type PodReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	eventChan    chan<- model.ResourceEventPayload
	clusterID    string
	agentVersion string
	filter       *ResourceFilter

	// Track last known state to detect changes
	podStates map[string]podState
}

type podState struct {
	phase           corev1.PodPhase
	ready           bool
	nodeName        string
	restartCount    int32
	resourceVersion string
}

func NewPodReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	eventChan chan<- model.ResourceEventPayload,
	clusterID, agentVersion string,
	filter *ResourceFilter,
) *PodReconciler {
	return &PodReconciler{
		Client:       client,
		Scheme:       scheme,
		Recorder:     recorder,
		eventChan:    eventChan,
		clusterID:    clusterID,
		agentVersion: agentVersion,
		filter:       filter,
		podStates:    make(map[string]podState),
	}
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Apply namespace filter
	if r.filter != nil && !r.filter.ShouldWatchNamespace(req.Namespace) {
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if apierrors.IsNotFound(err) {
			// Pod was deleted
			r.handleDeletion(ctx, req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Apply label filter
	if r.filter != nil && !r.filter.ShouldWatchResource(pod.Labels) {
		return ctrl.Result{}, nil
	}

	adapter := NewPodAdapter(pod)
	log.V(1).Info("Reconciling Pod", "namespace", req.Namespace, "name", req.Name, "phase", adapter.GetPhase())

	r.reconcilePod(ctx, adapter)

	return ctrl.Result{}, nil
}

func (r *PodReconciler) reconcilePod(ctx context.Context, adapter *PodAdapter) {
	log := ctrl.LoggerFrom(ctx)
	podKey := adapter.GetNamespace() + "/" + adapter.GetName()

	// Get current state
	currentState := podState{
		phase:           adapter.GetPhase(),
		ready:           adapter.IsReady(),
		nodeName:        adapter.GetNodeName(),
		restartCount:    adapter.getTotalRestartCount(),
		resourceVersion: adapter.Pod.ResourceVersion,
	}

	// Check if this is a new pod or state changed
	lastState, exists := r.podStates[podKey]
	if !exists {
		// New pod
		r.publishEvent(adapter, model.ResourceEventKindCreated)
		r.podStates[podKey] = currentState
		log.V(1).Info("Pod created", "pod", podKey, "phase", currentState.phase)
		return
	}

	// Check for meaningful state changes
	if r.hasStateChanged(lastState, currentState) {
		r.publishEvent(adapter, model.ResourceEventKindStatusChange)
		r.podStates[podKey] = currentState
		log.V(1).Info("Pod status changed",
			"pod", podKey,
			"phase", currentState.phase,
			"ready", currentState.ready,
			"restartCount", currentState.restartCount,
		)
	}
}

func (r *PodReconciler) hasStateChanged(last, current podState) bool {
	return last.phase != current.phase ||
		last.ready != current.ready ||
		last.nodeName != current.nodeName ||
		last.restartCount != current.restartCount
}

func (r *PodReconciler) handleDeletion(ctx context.Context, namespace, name string) {
	log := ctrl.LoggerFrom(ctx)
	podKey := namespace + "/" + name
	log.V(1).Info("Pod deleted", "pod", podKey)

	// Send deletion event
	event := model.NewResourceEventPayload(
		model.ResourceTypePod,
		model.ResourceRef{
			Kind:      "Pod",
			Name:      name,
			Namespace: namespace,
		},
		nil,
		model.ResourceEventKindDeleted,
		nil,
		nil,
		r.clusterID,
		r.agentVersion,
	)

	select {
	case r.eventChan <- event:
	default:
		log.Error(nil, "Event channel full, dropping pod deletion event", "pod", podKey)
	}

	delete(r.podStates, podKey)
}

func (r *PodReconciler) publishEvent(adapter *PodAdapter, eventKind model.ResourceEventKind) {
	event := model.NewPodEvent(
		adapter.GetNamespace(),
		adapter.GetName(),
		adapter.GetUID(),
		adapter.GetLabels(),
		eventKind,
		adapter.GetState(),
		r.extractPodMetadata(adapter),
		r.clusterID,
		r.agentVersion,
	)

	select {
	case r.eventChan <- event:
	default:
		// Log if channel is full but don't block
		ctrl.Log.Error(nil, "Event channel full, dropping pod event",
			"pod", adapter.GetNamespace()+"/"+adapter.GetName(),
			"eventKind", eventKind,
		)
	}
}

func (r *PodReconciler) extractPodMetadata(adapter *PodAdapter) *model.PodMetadata {
	meta := adapter.GetMetadata()
	if pm, ok := meta["pod"].(*model.PodMetadata); ok {
		return pm
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
