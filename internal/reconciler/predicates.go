package reconciler

import (
	v1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// DeploymentStatusChangedPredicate allows generation changes and status changes
// that affect rollout phase detection (replicas, conditions, observed generation).
func DeploymentStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return true },
		GenericFunc: func(e event.GenericEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, okOld := e.ObjectOld.(*v1.Deployment)
			newObj, okNew := e.ObjectNew.(*v1.Deployment)
			if !okOld || !okNew {
				return true
			}
			if oldObj.Generation != newObj.Generation {
				return true
			}
			return deploymentStatusChanged(oldObj, newObj)
		},
	}
}

// deploymentStatusChanged returns true if any status field relevant to rollout phase changed.
func deploymentStatusChanged(oldObj, newObj *v1.Deployment) bool {
	oldStatus := oldObj.Status
	newStatus := newObj.Status

	if oldStatus.Replicas != newStatus.Replicas {
		return true
	}
	if oldStatus.UpdatedReplicas != newStatus.UpdatedReplicas {
		return true
	}
	if oldStatus.ReadyReplicas != newStatus.ReadyReplicas {
		return true
	}
	if oldStatus.AvailableReplicas != newStatus.AvailableReplicas {
		return true
	}
	if oldStatus.ObservedGeneration != newStatus.ObservedGeneration {
		return true
	}

	// Check conditions for changes in type, status, or reason
	if len(oldStatus.Conditions) != len(newStatus.Conditions) {
		return true
	}
	oldConditions := make(map[v1.DeploymentConditionType]v1.DeploymentCondition)
	for _, c := range oldStatus.Conditions {
		oldConditions[c.Type] = c
	}
	for _, newCond := range newStatus.Conditions {
		oldCond, exists := oldConditions[newCond.Type]
		if !exists {
			return true
		}
		if oldCond.Status != newCond.Status || oldCond.Reason != newCond.Reason {
			return true
		}
	}

	return false
}

// StatefulSetStatusChangedPredicate allows generation changes and status changes
// that affect rollout phase detection.
func StatefulSetStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return true },
		GenericFunc: func(e event.GenericEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, okOld := e.ObjectOld.(*v1.StatefulSet)
			newObj, okNew := e.ObjectNew.(*v1.StatefulSet)
			if !okOld || !okNew {
				return true
			}
			if oldObj.Generation != newObj.Generation {
				return true
			}
			return statefulSetStatusChanged(oldObj, newObj)
		},
	}
}

// statefulSetStatusChanged returns true if any status field relevant to rollout phase changed.
func statefulSetStatusChanged(oldObj, newObj *v1.StatefulSet) bool {
	oldStatus := oldObj.Status
	newStatus := newObj.Status

	if oldStatus.Replicas != newStatus.Replicas {
		return true
	}
	if oldStatus.UpdatedReplicas != newStatus.UpdatedReplicas {
		return true
	}
	if oldStatus.ReadyReplicas != newStatus.ReadyReplicas {
		return true
	}
	if oldStatus.CurrentReplicas != newStatus.CurrentReplicas {
		return true
	}
	if oldStatus.AvailableReplicas != newStatus.AvailableReplicas {
		return true
	}
	if oldStatus.ObservedGeneration != newStatus.ObservedGeneration {
		return true
	}

	return false
}

// DaemonSetStatusChangedPredicate allows generation changes and status changes
// that affect rollout phase detection.
func DaemonSetStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return true },
		GenericFunc: func(e event.GenericEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, okOld := e.ObjectOld.(*v1.DaemonSet)
			newObj, okNew := e.ObjectNew.(*v1.DaemonSet)
			if !okOld || !okNew {
				return true
			}
			if oldObj.Generation != newObj.Generation {
				return true
			}
			return daemonSetStatusChanged(oldObj, newObj)
		},
	}
}

// daemonSetStatusChanged returns true if any status field relevant to rollout phase changed.
func daemonSetStatusChanged(oldObj, newObj *v1.DaemonSet) bool {
	oldStatus := oldObj.Status
	newStatus := newObj.Status

	if oldStatus.DesiredNumberScheduled != newStatus.DesiredNumberScheduled {
		return true
	}
	if oldStatus.CurrentNumberScheduled != newStatus.CurrentNumberScheduled {
		return true
	}
	if oldStatus.UpdatedNumberScheduled != newStatus.UpdatedNumberScheduled {
		return true
	}
	if oldStatus.NumberReady != newStatus.NumberReady {
		return true
	}
	if oldStatus.NumberAvailable != newStatus.NumberAvailable {
		return true
	}
	if oldStatus.NumberUnavailable != newStatus.NumberUnavailable {
		return true
	}
	if oldStatus.ObservedGeneration != newStatus.ObservedGeneration {
		return true
	}

	return false
}
