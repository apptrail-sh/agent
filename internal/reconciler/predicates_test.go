package reconciler

import (
	"testing"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDeploymentStatusChangedPredicate(t *testing.T) {
	pred := DeploymentStatusChangedPredicate()

	baseDeployment := func() *v1.Deployment {
		return &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-deployment",
				Namespace:  "default",
				Generation: 1,
			},
			Status: v1.DeploymentStatus{
				Replicas:           3,
				UpdatedReplicas:    3,
				ReadyReplicas:      3,
				AvailableReplicas:  3,
				ObservedGeneration: 1,
				Conditions: []v1.DeploymentCondition{
					{Type: v1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"},
					{Type: v1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "NewReplicaSetAvailable"},
				},
			},
		}
	}

	tests := []struct {
		name     string
		modify   func(old, new *v1.Deployment)
		expected bool
	}{
		{
			name: "generation changed",
			modify: func(old, new *v1.Deployment) {
				new.Generation = 2
			},
			expected: true,
		},
		{
			name: "replicas changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.Replicas = 4
			},
			expected: true,
		},
		{
			name: "updated replicas changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.UpdatedReplicas = 2
			},
			expected: true,
		},
		{
			name: "ready replicas changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.ReadyReplicas = 2
			},
			expected: true,
		},
		{
			name: "available replicas changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.AvailableReplicas = 2
			},
			expected: true,
		},
		{
			name: "observed generation changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.ObservedGeneration = 2
			},
			expected: true,
		},
		{
			name: "condition status changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.Conditions[0].Status = corev1.ConditionFalse
			},
			expected: true,
		},
		{
			name: "condition reason changed",
			modify: func(old, new *v1.Deployment) {
				new.Status.Conditions[0].Reason = "DifferentReason"
			},
			expected: true,
		},
		{
			name: "condition added",
			modify: func(old, new *v1.Deployment) {
				new.Status.Conditions = append(new.Status.Conditions, v1.DeploymentCondition{
					Type: v1.DeploymentReplicaFailure, Status: corev1.ConditionTrue,
				})
			},
			expected: true,
		},
		{
			name: "no relevant change",
			modify: func(old, new *v1.Deployment) {
				// Only change labels, which shouldn't trigger
				new.Labels = map[string]string{"foo": "bar"}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := baseDeployment()
			new := baseDeployment()
			tt.modify(old, new)

			e := event.UpdateEvent{
				ObjectOld: old,
				ObjectNew: new,
			}

			got := pred.Update(e)
			if got != tt.expected {
				t.Errorf("DeploymentStatusChangedPredicate.Update() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDeploymentStatusChangedPredicate_OtherEvents(t *testing.T) {
	pred := DeploymentStatusChangedPredicate()

	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	if !pred.Create(event.CreateEvent{Object: deployment}) {
		t.Error("CreateFunc should return true")
	}
	if !pred.Delete(event.DeleteEvent{Object: deployment}) {
		t.Error("DeleteFunc should return true")
	}
	if !pred.Generic(event.GenericEvent{Object: deployment}) {
		t.Error("GenericFunc should return true")
	}
}

func TestStatefulSetStatusChangedPredicate(t *testing.T) {
	pred := StatefulSetStatusChangedPredicate()

	baseStatefulSet := func() *v1.StatefulSet {
		return &v1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-statefulset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: v1.StatefulSetStatus{
				Replicas:           3,
				UpdatedReplicas:    3,
				ReadyReplicas:      3,
				CurrentReplicas:    3,
				AvailableReplicas:  3,
				ObservedGeneration: 1,
			},
		}
	}

	tests := []struct {
		name     string
		modify   func(old, new *v1.StatefulSet)
		expected bool
	}{
		{
			name: "generation changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Generation = 2
			},
			expected: true,
		},
		{
			name: "replicas changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.Replicas = 4
			},
			expected: true,
		},
		{
			name: "updated replicas changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.UpdatedReplicas = 2
			},
			expected: true,
		},
		{
			name: "ready replicas changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.ReadyReplicas = 2
			},
			expected: true,
		},
		{
			name: "current replicas changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.CurrentReplicas = 2
			},
			expected: true,
		},
		{
			name: "available replicas changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.AvailableReplicas = 2
			},
			expected: true,
		},
		{
			name: "observed generation changed",
			modify: func(old, new *v1.StatefulSet) {
				new.Status.ObservedGeneration = 2
			},
			expected: true,
		},
		{
			name: "no relevant change",
			modify: func(old, new *v1.StatefulSet) {
				new.Labels = map[string]string{"foo": "bar"}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := baseStatefulSet()
			new := baseStatefulSet()
			tt.modify(old, new)

			e := event.UpdateEvent{
				ObjectOld: old,
				ObjectNew: new,
			}

			got := pred.Update(e)
			if got != tt.expected {
				t.Errorf("StatefulSetStatusChangedPredicate.Update() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatefulSetStatusChangedPredicate_OtherEvents(t *testing.T) {
	pred := StatefulSetStatusChangedPredicate()

	statefulset := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	if !pred.Create(event.CreateEvent{Object: statefulset}) {
		t.Error("CreateFunc should return true")
	}
	if !pred.Delete(event.DeleteEvent{Object: statefulset}) {
		t.Error("DeleteFunc should return true")
	}
	if !pred.Generic(event.GenericEvent{Object: statefulset}) {
		t.Error("GenericFunc should return true")
	}
}

func TestDaemonSetStatusChangedPredicate(t *testing.T) {
	pred := DaemonSetStatusChangedPredicate()

	baseDaemonSet := func() *v1.DaemonSet {
		return &v1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-daemonset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: v1.DaemonSetStatus{
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 3,
				NumberReady:            3,
				NumberAvailable:        3,
				NumberUnavailable:      0,
				ObservedGeneration:     1,
			},
		}
	}

	tests := []struct {
		name     string
		modify   func(old, new *v1.DaemonSet)
		expected bool
	}{
		{
			name: "generation changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Generation = 2
			},
			expected: true,
		},
		{
			name: "desired number scheduled changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.DesiredNumberScheduled = 4
			},
			expected: true,
		},
		{
			name: "current number scheduled changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.CurrentNumberScheduled = 2
			},
			expected: true,
		},
		{
			name: "updated number scheduled changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.UpdatedNumberScheduled = 2
			},
			expected: true,
		},
		{
			name: "number ready changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.NumberReady = 2
			},
			expected: true,
		},
		{
			name: "number available changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.NumberAvailable = 2
			},
			expected: true,
		},
		{
			name: "number unavailable changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.NumberUnavailable = 1
			},
			expected: true,
		},
		{
			name: "observed generation changed",
			modify: func(old, new *v1.DaemonSet) {
				new.Status.ObservedGeneration = 2
			},
			expected: true,
		},
		{
			name: "no relevant change",
			modify: func(old, new *v1.DaemonSet) {
				new.Labels = map[string]string{"foo": "bar"}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := baseDaemonSet()
			new := baseDaemonSet()
			tt.modify(old, new)

			e := event.UpdateEvent{
				ObjectOld: old,
				ObjectNew: new,
			}

			got := pred.Update(e)
			if got != tt.expected {
				t.Errorf("DaemonSetStatusChangedPredicate.Update() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDaemonSetStatusChangedPredicate_OtherEvents(t *testing.T) {
	pred := DaemonSetStatusChangedPredicate()

	daemonset := &v1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	if !pred.Create(event.CreateEvent{Object: daemonset}) {
		t.Error("CreateFunc should return true")
	}
	if !pred.Delete(event.DeleteEvent{Object: daemonset}) {
		t.Error("DeleteFunc should return true")
	}
	if !pred.Generic(event.GenericEvent{Object: daemonset}) {
		t.Error("GenericFunc should return true")
	}
}

func TestPredicates_WrongType(t *testing.T) {
	// Test that predicates return true when given wrong object types
	deployment := &v1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	statefulset := &v1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	daemonset := &v1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	// DeploymentStatusChangedPredicate with wrong type
	depPred := DeploymentStatusChangedPredicate()
	if !depPred.Update(event.UpdateEvent{ObjectOld: statefulset, ObjectNew: statefulset}) {
		t.Error("DeploymentStatusChangedPredicate should return true for wrong type")
	}

	// StatefulSetStatusChangedPredicate with wrong type
	ssPred := StatefulSetStatusChangedPredicate()
	if !ssPred.Update(event.UpdateEvent{ObjectOld: deployment, ObjectNew: deployment}) {
		t.Error("StatefulSetStatusChangedPredicate should return true for wrong type")
	}

	// DaemonSetStatusChangedPredicate with wrong type
	dsPred := DaemonSetStatusChangedPredicate()
	if !dsPred.Update(event.UpdateEvent{ObjectOld: daemonset, ObjectNew: deployment}) {
		t.Error("DaemonSetStatusChangedPredicate should return true for wrong type")
	}
}
