/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadRolloutStateSpec defines the desired state of WorkloadRolloutState
type WorkloadRolloutStateSpec struct {
	// WorkloadNamespace is the namespace of the workload being tracked
	// +required
	WorkloadNamespace string `json:"workloadNamespace"`

	// WorkloadName is the name of the workload being tracked
	// +required
	WorkloadName string `json:"workloadName"`

	// WorkloadKind is the kind of workload (Deployment, StatefulSet, DaemonSet)
	// +required
	WorkloadKind string `json:"workloadKind"`

	// RolloutStarted is the timestamp when the rollout started
	// +required
	RolloutStarted metav1.Time `json:"rolloutStarted"`

	// Version is the version being rolled out (app.kubernetes.io/version label)
	// +optional
	Version string `json:"version,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadRolloutState is the Schema for the workloadrolloutstates API
// This resource tracks rollout timing state for workloads (Deployments, StatefulSets, DaemonSets) across the cluster
type WorkloadRolloutState struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of WorkloadRolloutState
	// +required
	Spec WorkloadRolloutStateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// WorkloadRolloutStateList contains a list of WorkloadRolloutState
type WorkloadRolloutStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WorkloadRolloutState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadRolloutState{}, &WorkloadRolloutStateList{})
}
