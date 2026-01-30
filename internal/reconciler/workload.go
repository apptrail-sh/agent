package reconciler

import (
	"github.com/apptrail-sh/agent/internal/model"
	v1 "k8s.io/api/apps/v1"
)

// WorkloadAdapter abstracts the common operations across Deployments, StatefulSets, and DaemonSets
// It implements WorkloadResourceAdapter interface
type WorkloadAdapter interface {
	WorkloadResourceAdapter
}

// DeploymentAdapter wraps a Deployment to implement WorkloadAdapter
type DeploymentAdapter struct {
	Deployment *v1.Deployment
}

func (d *DeploymentAdapter) GetName() string {
	return d.Deployment.Name
}

func (d *DeploymentAdapter) GetNamespace() string {
	return d.Deployment.Namespace
}

func (d *DeploymentAdapter) GetKind() string {
	return "Deployment"
}

func (d *DeploymentAdapter) GetLabels() map[string]string {
	return d.Deployment.Labels
}

func (d *DeploymentAdapter) GetVersion() string {
	return d.Deployment.Labels["app.kubernetes.io/version"]
}

func (d *DeploymentAdapter) GetTotalReplicas() int32 {
	return d.Deployment.Status.Replicas
}

func (d *DeploymentAdapter) GetReadyReplicas() int32 {
	return d.Deployment.Status.ReadyReplicas
}

func (d *DeploymentAdapter) GetUpdatedReplicas() int32 {
	return d.Deployment.Status.UpdatedReplicas
}

func (d *DeploymentAdapter) GetAvailableReplicas() int32 {
	return d.Deployment.Status.AvailableReplicas
}

func (d *DeploymentAdapter) IsRollingOut() bool {
	return d.Deployment.Status.UpdatedReplicas < d.Deployment.Status.Replicas ||
		d.Deployment.Status.ReadyReplicas < d.Deployment.Status.Replicas
}

func (d *DeploymentAdapter) HasFailed() bool {
	for _, condition := range d.Deployment.Status.Conditions {
		switch condition.Type {
		case v1.DeploymentProgressing:
			if condition.Status == "False" {
				return true
			}
			if condition.Reason == "ProgressDeadlineExceeded" {
				return true
			}
		}
	}
	return false
}

func (d *DeploymentAdapter) GetUID() string {
	return string(d.Deployment.UID)
}

func (d *DeploymentAdapter) GetResourceType() model.ResourceType {
	return model.ResourceTypeWorkload
}

// StatefulSetAdapter wraps a StatefulSet to implement WorkloadAdapter
type StatefulSetAdapter struct {
	StatefulSet *v1.StatefulSet
}

func (s *StatefulSetAdapter) GetName() string {
	return s.StatefulSet.Name
}

func (s *StatefulSetAdapter) GetNamespace() string {
	return s.StatefulSet.Namespace
}

func (s *StatefulSetAdapter) GetKind() string {
	return "StatefulSet"
}

func (s *StatefulSetAdapter) GetLabels() map[string]string {
	return s.StatefulSet.Labels
}

func (s *StatefulSetAdapter) GetVersion() string {
	return s.StatefulSet.Labels["app.kubernetes.io/version"]
}

func (s *StatefulSetAdapter) GetTotalReplicas() int32 {
	return s.StatefulSet.Status.Replicas
}

func (s *StatefulSetAdapter) GetReadyReplicas() int32 {
	return s.StatefulSet.Status.ReadyReplicas
}

func (s *StatefulSetAdapter) GetUpdatedReplicas() int32 {
	return s.StatefulSet.Status.UpdatedReplicas
}

func (s *StatefulSetAdapter) GetAvailableReplicas() int32 {
	return s.StatefulSet.Status.AvailableReplicas
}

func (s *StatefulSetAdapter) IsRollingOut() bool {
	// StatefulSet is rolling out if updated replicas don't match desired
	// or if not all replicas are ready
	if s.StatefulSet.Spec.Replicas == nil {
		return false
	}
	desiredReplicas := *s.StatefulSet.Spec.Replicas
	return s.StatefulSet.Status.UpdatedReplicas < desiredReplicas ||
		s.StatefulSet.Status.ReadyReplicas < desiredReplicas
}

func (s *StatefulSetAdapter) HasFailed() bool {
	// StatefulSets don't have explicit failure conditions like Deployments
	// We rely on timeout-based failure detection
	return false
}

func (s *StatefulSetAdapter) GetUID() string {
	return string(s.StatefulSet.UID)
}

func (s *StatefulSetAdapter) GetResourceType() model.ResourceType {
	return model.ResourceTypeWorkload
}

// DaemonSetAdapter wraps a DaemonSet to implement WorkloadAdapter
type DaemonSetAdapter struct {
	DaemonSet *v1.DaemonSet
}

func (d *DaemonSetAdapter) GetName() string {
	return d.DaemonSet.Name
}

func (d *DaemonSetAdapter) GetNamespace() string {
	return d.DaemonSet.Namespace
}

func (d *DaemonSetAdapter) GetKind() string {
	return "DaemonSet"
}

func (d *DaemonSetAdapter) GetLabels() map[string]string {
	return d.DaemonSet.Labels
}

func (d *DaemonSetAdapter) GetVersion() string {
	return d.DaemonSet.Labels["app.kubernetes.io/version"]
}

func (d *DaemonSetAdapter) GetTotalReplicas() int32 {
	// DaemonSets use DesiredNumberScheduled instead of Replicas
	return d.DaemonSet.Status.DesiredNumberScheduled
}

func (d *DaemonSetAdapter) GetReadyReplicas() int32 {
	return d.DaemonSet.Status.NumberReady
}

func (d *DaemonSetAdapter) GetUpdatedReplicas() int32 {
	return d.DaemonSet.Status.UpdatedNumberScheduled
}

func (d *DaemonSetAdapter) GetAvailableReplicas() int32 {
	return d.DaemonSet.Status.NumberAvailable
}

func (d *DaemonSetAdapter) IsRollingOut() bool {
	// DaemonSet is rolling out if not all scheduled pods are updated or ready
	return d.DaemonSet.Status.UpdatedNumberScheduled < d.DaemonSet.Status.DesiredNumberScheduled ||
		d.DaemonSet.Status.NumberReady < d.DaemonSet.Status.DesiredNumberScheduled
}

func (d *DaemonSetAdapter) HasFailed() bool {
	// DaemonSets don't have explicit failure conditions
	// We rely on timeout-based failure detection
	return false
}

func (d *DaemonSetAdapter) GetUID() string {
	return string(d.DaemonSet.UID)
}

func (d *DaemonSetAdapter) GetResourceType() model.ResourceType {
	return model.ResourceTypeWorkload
}
