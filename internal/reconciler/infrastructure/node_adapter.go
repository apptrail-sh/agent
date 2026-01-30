package infrastructure

import (
	"github.com/apptrail-sh/agent/internal/model"
	corev1 "k8s.io/api/core/v1"
)

// NodeAdapter wraps a Node to implement InfrastructureResourceAdapter
type NodeAdapter struct {
	Node *corev1.Node
}

func NewNodeAdapter(node *corev1.Node) *NodeAdapter {
	return &NodeAdapter{Node: node}
}

func (n *NodeAdapter) GetName() string {
	return n.Node.Name
}

func (n *NodeAdapter) GetNamespace() string {
	return "" // Nodes are cluster-scoped
}

func (n *NodeAdapter) GetKind() string {
	return "Node"
}

func (n *NodeAdapter) GetUID() string {
	return string(n.Node.UID)
}

func (n *NodeAdapter) GetLabels() map[string]string {
	return n.Node.Labels
}

func (n *NodeAdapter) GetResourceType() model.ResourceType {
	return model.ResourceTypeNode
}

func (n *NodeAdapter) GetState() *model.ResourceState {
	conditions := make([]model.Condition, 0, len(n.Node.Status.Conditions))
	for _, c := range n.Node.Status.Conditions {
		conditions = append(conditions, model.Condition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	return &model.ResourceState{
		Phase:      string(n.Node.Status.Phase),
		Conditions: conditions,
	}
}

func (n *NodeAdapter) GetMetadata() map[string]any {
	nodeInfo := n.Node.Status.NodeInfo

	capacity := make(map[string]string)
	for k, v := range n.Node.Status.Capacity {
		capacity[string(k)] = v.String()
	}

	allocatable := make(map[string]string)
	for k, v := range n.Node.Status.Allocatable {
		allocatable[string(k)] = v.String()
	}

	taints := make([]model.NodeTaint, 0, len(n.Node.Spec.Taints))
	for _, t := range n.Node.Spec.Taints {
		taints = append(taints, model.NodeTaint{
			Key:    t.Key,
			Value:  t.Value,
			Effect: string(t.Effect),
		})
	}

	nodeMetadata := &model.NodeMetadata{
		KubeletVersion:          nodeInfo.KubeletVersion,
		ContainerRuntimeVersion: nodeInfo.ContainerRuntimeVersion,
		OSImage:                 nodeInfo.OSImage,
		Architecture:            nodeInfo.Architecture,
		Capacity:                capacity,
		Allocatable:             allocatable,
		Taints:                  taints,
	}

	return map[string]any{
		"node": nodeMetadata,
	}
}

// IsReady returns true if the node is in Ready condition
func (n *NodeAdapter) IsReady() bool {
	for _, c := range n.Node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// HasPressure returns true if the node has any resource pressure conditions
func (n *NodeAdapter) HasPressure() bool {
	for _, c := range n.Node.Status.Conditions {
		switch c.Type {
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

// IsUnschedulable returns true if the node is cordoned
func (n *NodeAdapter) IsUnschedulable() bool {
	return n.Node.Spec.Unschedulable
}
