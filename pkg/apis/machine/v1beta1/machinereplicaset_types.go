/*
Copyright 2018 The Kubernetes Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

/// [MachineReplicaSet]
// MachineReplicaSet is the Schema for the machineReplicaSet API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type MachineReplicaSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MachineReplicaSetSpec   `json:"spec,omitempty"`
	Status MachineReplicaSetStatus `json:"status,omitempty"`
}

/// [MachineReplicaSet]

/// [MachineReplicaSetSpec]
// MachineReplicaSetSpec is spec for mcps
type MachineReplicaSetSpec struct {
	// MinReplicas is the number of replicas that need to be present to operate.
	// This is a pointer to distinguish between explicit zero and unspecified.
	// Defaults to 3.
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// Selector is a label query over machines that should match the replica count.
	// Label keys and values that must match in order to be controlled by this MachineSet.
	// It must match the machine template's labels.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	Selector metav1.LabelSelector `json:"selector"`
}

/// [MachineReplicaSetSpec]

/// [MachineReplicaSetStatus]
// MachineReplicaSetStatus defines the observed state of MachineReplicaSet
type MachineReplicaSetStatus struct {
	// Replicas is the most recently observed number of replicas.
	Replicas []MachineReplica `json:"replicas,omitempty"`
}

/// [MachineReplicaSetStatus]

type MachineReplica struct {
	Name                  string `json:"name"`
	Replaces              string `json:"replaces,omitempty"`
	ReplacementInProgress bool   `json:"replacementInProgress"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineReplicaSetList contains a list of MachineReplicaSet
type MachineReplicaSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MachineReplicaSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MachineReplicaSet{}, &MachineReplicaSetList{})
}
