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

// NodeReconciler reconciles Node objects
type NodeReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	eventChan    chan<- model.ResourceEventPayload
	clusterID    string
	agentVersion string

	// Track last known state to detect changes
	nodeStates map[string]nodeState
}

type nodeState struct {
	ready           bool
	unschedulable   bool
	hasPressure     bool
	kubeletVersion  string
	resourceVersion string
}

func NewNodeReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	eventChan chan<- model.ResourceEventPayload,
	clusterID, agentVersion string,
) *NodeReconciler {
	return &NodeReconciler{
		Client:       client,
		Scheme:       scheme,
		Recorder:     recorder,
		eventChan:    eventChan,
		clusterID:    clusterID,
		agentVersion: agentVersion,
		nodeStates:   make(map[string]nodeState),
	}
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling Node", "name", req.Name)

	node := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// Node was deleted
			r.handleDeletion(ctx, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	adapter := NewNodeAdapter(node)
	r.reconcileNode(ctx, adapter)

	return ctrl.Result{}, nil
}

func (r *NodeReconciler) reconcileNode(ctx context.Context, adapter *NodeAdapter) {
	log := ctrl.LoggerFrom(ctx)
	nodeName := adapter.GetName()

	// Get current state
	currentState := nodeState{
		ready:           adapter.IsReady(),
		unschedulable:   adapter.IsUnschedulable(),
		hasPressure:     adapter.HasPressure(),
		kubeletVersion:  adapter.Node.Status.NodeInfo.KubeletVersion,
		resourceVersion: adapter.Node.ResourceVersion,
	}

	// Check if this is a new node or state changed
	lastState, exists := r.nodeStates[nodeName]
	if !exists {
		// New node
		r.publishEvent(adapter, model.ResourceEventKindCreated)
		r.nodeStates[nodeName] = currentState
		log.Info("Node created", "node", nodeName)
		return
	}

	// Check for meaningful state changes
	if r.hasStateChanged(lastState, currentState) {
		r.publishEvent(adapter, model.ResourceEventKindStatusChange)
		r.nodeStates[nodeName] = currentState
		log.Info("Node status changed",
			"node", nodeName,
			"ready", currentState.ready,
			"unschedulable", currentState.unschedulable,
			"hasPressure", currentState.hasPressure,
		)
	}
}

func (r *NodeReconciler) hasStateChanged(last, current nodeState) bool {
	return last.ready != current.ready ||
		last.unschedulable != current.unschedulable ||
		last.hasPressure != current.hasPressure ||
		last.kubeletVersion != current.kubeletVersion
}

func (r *NodeReconciler) handleDeletion(ctx context.Context, nodeName string) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Node deleted", "node", nodeName)

	// Send deletion event
	event := model.NewResourceEventPayload(
		model.ResourceTypeNode,
		model.ResourceRef{
			Kind: "Node",
			Name: nodeName,
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
		log.Error(nil, "Event channel full, dropping node deletion event", "node", nodeName)
	}

	delete(r.nodeStates, nodeName)
}

func (r *NodeReconciler) publishEvent(adapter *NodeAdapter, eventKind model.ResourceEventKind) {
	event := model.NewNodeEvent(
		adapter.GetName(),
		adapter.GetUID(),
		adapter.GetLabels(),
		eventKind,
		adapter.GetState(),
		r.extractNodeMetadata(adapter),
		r.clusterID,
		r.agentVersion,
	)

	select {
	case r.eventChan <- event:
	default:
		// Log if channel is full but don't block
		ctrl.Log.Error(nil, "Event channel full, dropping node event",
			"node", adapter.GetName(),
			"eventKind", eventKind,
		)
	}
}

func (r *NodeReconciler) extractNodeMetadata(adapter *NodeAdapter) *model.NodeMetadata {
	meta := adapter.GetMetadata()
	if nm, ok := meta["node"].(*model.NodeMetadata); ok {
		return nm
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
