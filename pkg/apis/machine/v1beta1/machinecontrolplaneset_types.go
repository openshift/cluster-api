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

/// [MachineControlPlaneSet]
// MachineControlPlaneSet is the Schema for the machineControlPlaneSet API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type MachineControlPlaneSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MachineControlPlaneSetSpec   `json:"spec,omitempty"`
	Status MachineControlPlaneSetStatus `json:"status,omitempty"`
}

/// [MachineControlPlaneSet]

/// [MachineControlPlaneSetSpec]
// MachineControlPlaneSetSpec is spec for mcps
type MachineControlPlaneSetSpec struct {
	// PlaceHolder doesn't do much
	PlaceHolder string `json:"placeHolder,omitempty"`
}

/// [MachineControlPlaneSetSpec]

/// [MachineControlPlaneSetStatus]
// MachineControlPlaneSetStatus defines the observed state of MachineControlPlaneSet
type MachineControlPlaneSetStatus struct {
	// Replicas is the most recently observed number of replicas.
	ControlPlaneMachines []ControlPlaneMachine `json:"controlPlaneMachines,omitempty"`
}

/// [MachineControlPlaneSetStatus]

type ControlPlaneMachine struct {
	Name string `json:"name"`
	Replaces string `json:"replaces,omitempty"`
	ReplacementInProgress bool `json:"replacementInProgress"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineControlPlaneSetList contains a list of MachineControlPlaneSet
type MachineControlPlaneSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MachineControlPlaneSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MachineControlPlaneSet{}, &MachineControlPlaneSetList{})
}
