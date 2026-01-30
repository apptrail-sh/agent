package reconciler

import (
	"github.com/apptrail-sh/agent/internal/model"
)

// ResourceAdapter is the base interface for all Kubernetes resource adapters
type ResourceAdapter interface {
	GetName() string
	GetNamespace() string // Empty for cluster-scoped resources
	GetKind() string
	GetUID() string
	GetLabels() map[string]string
	GetResourceType() model.ResourceType
}

// WorkloadResourceAdapter extends ResourceAdapter for workload-type resources
// (Deployments, StatefulSets, DaemonSets)
type WorkloadResourceAdapter interface {
	ResourceAdapter

	// Version tracking
	GetVersion() string // Gets app.kubernetes.io/version label

	// Replica status
	GetTotalReplicas() int32
	GetReadyReplicas() int32
	GetUpdatedReplicas() int32
	GetAvailableReplicas() int32

	// Phase determination
	IsRollingOut() bool
	HasFailed() bool
}

// InfrastructureResourceAdapter extends ResourceAdapter for infrastructure resources
// (Nodes, Pods, Services)
type InfrastructureResourceAdapter interface {
	ResourceAdapter

	// State extraction
	GetState() *model.ResourceState
	GetMetadata() map[string]any
}
