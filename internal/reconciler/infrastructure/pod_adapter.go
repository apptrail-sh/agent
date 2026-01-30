package infrastructure

import (
	"github.com/apptrail-sh/agent/internal/model"
	corev1 "k8s.io/api/core/v1"
)

// PodAdapter wraps a Pod to implement InfrastructureResourceAdapter
type PodAdapter struct {
	Pod *corev1.Pod
}

func NewPodAdapter(pod *corev1.Pod) *PodAdapter {
	return &PodAdapter{Pod: pod}
}

func (p *PodAdapter) GetName() string {
	return p.Pod.Name
}

func (p *PodAdapter) GetNamespace() string {
	return p.Pod.Namespace
}

func (p *PodAdapter) GetKind() string {
	return "Pod"
}

func (p *PodAdapter) GetUID() string {
	return string(p.Pod.UID)
}

func (p *PodAdapter) GetLabels() map[string]string {
	return p.Pod.Labels
}

func (p *PodAdapter) GetResourceType() model.ResourceType {
	return model.ResourceTypePod
}

func (p *PodAdapter) GetState() *model.ResourceState {
	conditions := make([]model.Condition, 0, len(p.Pod.Status.Conditions))
	for _, c := range p.Pod.Status.Conditions {
		conditions = append(conditions, model.Condition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	return &model.ResourceState{
		Phase:      string(p.Pod.Status.Phase),
		Conditions: conditions,
	}
}

func (p *PodAdapter) GetMetadata() map[string]any {
	podMetadata := &model.PodMetadata{
		NodeName:       p.Pod.Spec.NodeName,
		PodIP:          p.Pod.Status.PodIP,
		RestartCount:   p.getTotalRestartCount(),
		Containers:     p.getContainerStatuses(p.Pod.Status.ContainerStatuses),
		InitContainers: p.getContainerStatuses(p.Pod.Status.InitContainerStatuses),
	}

	if p.Pod.Status.StartTime != nil {
		startTime := p.Pod.Status.StartTime.Time
		podMetadata.StartTime = &startTime
	}

	// Extract owner reference (typically ReplicaSet -> Deployment)
	if len(p.Pod.OwnerReferences) > 0 {
		owner := p.Pod.OwnerReferences[0]
		podMetadata.OwnerKind = owner.Kind
		podMetadata.OwnerName = owner.Name
		podMetadata.OwnerUID = string(owner.UID)
	}

	return map[string]any{
		"pod": podMetadata,
	}
}

func (p *PodAdapter) getContainerStatuses(statuses []corev1.ContainerStatus) []model.ContainerStatus {
	result := make([]model.ContainerStatus, 0, len(statuses))
	for _, cs := range statuses {
		containerStatus := model.ContainerStatus{
			Name:         cs.Name,
			Image:        cs.Image,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}

		// Determine state and reason
		if cs.State.Running != nil {
			containerStatus.State = "running"
		} else if cs.State.Waiting != nil {
			containerStatus.State = "waiting"
			containerStatus.Reason = cs.State.Waiting.Reason
			containerStatus.Message = cs.State.Waiting.Message
		} else if cs.State.Terminated != nil {
			containerStatus.State = "terminated"
			containerStatus.Reason = cs.State.Terminated.Reason
			containerStatus.Message = cs.State.Terminated.Message
		}

		result = append(result, containerStatus)
	}
	return result
}

func (p *PodAdapter) getTotalRestartCount() int32 {
	var total int32
	for _, cs := range p.Pod.Status.ContainerStatuses {
		total += cs.RestartCount
	}
	return total
}

// GetNodeName returns the node where the pod is scheduled
func (p *PodAdapter) GetNodeName() string {
	return p.Pod.Spec.NodeName
}

// GetOwnerReference returns the first owner reference if present
func (p *PodAdapter) GetOwnerReference() (kind, name, uid string) {
	if len(p.Pod.OwnerReferences) > 0 {
		owner := p.Pod.OwnerReferences[0]
		return owner.Kind, owner.Name, string(owner.UID)
	}
	return "", "", ""
}

// IsReady returns true if the pod is in Ready condition
func (p *PodAdapter) IsReady() bool {
	for _, c := range p.Pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// IsTerminating returns true if the pod has a deletion timestamp
func (p *PodAdapter) IsTerminating() bool {
	return p.Pod.DeletionTimestamp != nil
}

// GetPhase returns the current pod phase
func (p *PodAdapter) GetPhase() corev1.PodPhase {
	return p.Pod.Status.Phase
}
