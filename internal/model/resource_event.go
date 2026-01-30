package model

import (
	"time"

	"github.com/google/uuid"
)

// ResourceType represents the type of Kubernetes resource
type ResourceType string

const (
	ResourceTypeWorkload ResourceType = "WORKLOAD"
	ResourceTypeNode     ResourceType = "NODE"
	ResourceTypePod      ResourceType = "POD"
	ResourceTypeService  ResourceType = "SERVICE"
)

// ResourceEventKind represents the type of event (lifecycle events)
type ResourceEventKind string

const (
	ResourceEventKindCreated      ResourceEventKind = "CREATED"
	ResourceEventKindUpdated      ResourceEventKind = "UPDATED"
	ResourceEventKindDeleted      ResourceEventKind = "DELETED"
	ResourceEventKindStatusChange ResourceEventKind = "STATUS_CHANGE"
)

// ResourceRef identifies a Kubernetes resource
type ResourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // Empty for cluster-scoped resources like nodes
	UID       string `json:"uid"`
}

// ResourceState holds the current state of a resource
type ResourceState struct {
	Phase      string            `json:"phase,omitempty"`
	Conditions []Condition       `json:"conditions,omitempty"`
	Metrics    map[string]string `json:"metrics,omitempty"`
}

// Condition represents a Kubernetes-style condition
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// NodeMetadata contains node-specific data
type NodeMetadata struct {
	KubeletVersion          string            `json:"kubeletVersion,omitempty"`
	ContainerRuntimeVersion string            `json:"containerRuntimeVersion,omitempty"`
	OSImage                 string            `json:"osImage,omitempty"`
	Architecture            string            `json:"architecture,omitempty"`
	Capacity                map[string]string `json:"capacity,omitempty"`
	Allocatable             map[string]string `json:"allocatable,omitempty"`
	Taints                  []NodeTaint       `json:"taints,omitempty"`
}

// NodeTaint represents a taint on a node
type NodeTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// PodMetadata contains pod-specific data
type PodMetadata struct {
	OwnerKind      string            `json:"ownerKind,omitempty"`
	OwnerName      string            `json:"ownerName,omitempty"`
	OwnerUID       string            `json:"ownerUID,omitempty"`
	NodeName       string            `json:"nodeName,omitempty"`
	PodIP          string            `json:"podIP,omitempty"`
	StartTime      *time.Time        `json:"startTime,omitempty"`
	RestartCount   int32             `json:"restartCount"`
	Containers     []ContainerStatus `json:"containers,omitempty"`
	InitContainers []ContainerStatus `json:"initContainers,omitempty"`
}

// ContainerStatus represents the status of a container in a pod
type ContainerStatus struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"` // running, waiting, terminated
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

// ResourceEventPayload is the generic event payload for all resource types
type ResourceEventPayload struct {
	EventID      string            `json:"eventId"`
	OccurredAt   time.Time         `json:"occurredAt"`
	Source       SourceMetadata    `json:"source"`
	ResourceType ResourceType      `json:"resourceType"`
	Resource     ResourceRef       `json:"resource"`
	Labels       map[string]string `json:"labels,omitempty"`
	EventKind    ResourceEventKind `json:"eventKind"`
	State        *ResourceState    `json:"state,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// NewResourceEventPayload creates a new resource event payload
func NewResourceEventPayload(
	resourceType ResourceType,
	resource ResourceRef,
	labels map[string]string,
	eventKind ResourceEventKind,
	state *ResourceState,
	metadata map[string]any,
	clusterID, agentVersion string,
) ResourceEventPayload {
	return ResourceEventPayload{
		EventID:    uuid.New().String(),
		OccurredAt: time.Now().UTC(),
		Source: SourceMetadata{
			ClusterID:    clusterID,
			AgentVersion: agentVersion,
		},
		ResourceType: resourceType,
		Resource:     resource,
		Labels:       labels,
		EventKind:    eventKind,
		State:        state,
		Metadata:     metadata,
	}
}

// NodeEvent is a convenience function for creating node events
func NewNodeEvent(
	name, uid string,
	labels map[string]string,
	eventKind ResourceEventKind,
	state *ResourceState,
	nodeMetadata *NodeMetadata,
	clusterID, agentVersion string,
) ResourceEventPayload {
	metadata := make(map[string]any)
	if nodeMetadata != nil {
		metadata["node"] = nodeMetadata
	}

	return NewResourceEventPayload(
		ResourceTypeNode,
		ResourceRef{
			Kind: "Node",
			Name: name,
			UID:  uid,
		},
		labels,
		eventKind,
		state,
		metadata,
		clusterID,
		agentVersion,
	)
}

// PodEvent is a convenience function for creating pod events
func NewPodEvent(
	namespace, name, uid string,
	labels map[string]string,
	eventKind ResourceEventKind,
	state *ResourceState,
	podMetadata *PodMetadata,
	clusterID, agentVersion string,
) ResourceEventPayload {
	metadata := make(map[string]any)
	if podMetadata != nil {
		metadata["pod"] = podMetadata
	}

	return NewResourceEventPayload(
		ResourceTypePod,
		ResourceRef{
			Kind:      "Pod",
			Name:      name,
			Namespace: namespace,
			UID:       uid,
		},
		labels,
		eventKind,
		state,
		metadata,
		clusterID,
		agentVersion,
	)
}
