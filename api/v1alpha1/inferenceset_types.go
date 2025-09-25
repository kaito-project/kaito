// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InferenceSetTemplate defines the template for creating InferenceSet instances.
type InferenceSetTemplate struct {
	Resource  ResourceSpec  `json:"resource,omitempty"`
	Inference InferenceSpec `json:"inference,omitempty"`
}

// InferenceSetSpec defines the desired state of InferenceSet
type InferenceSetSpec struct {
	// Template is the template used to create the InferenceSet.
	Template InferenceSetTemplate `json:"template"`
	// Replicas is the desired number of workspaces to be created.
	// +optional
	// +kubebuilder:default:=1
	Replicas *int `json:"replicas,omitempty"`
	// NodeCountLimit is the maximum number of GPU nodes that can be created for the InferenceSet.
	// If not specified, there is no limit on the number of GPU nodes that can be created.
	// +optional
	NodeCountLimit *int `json:"nodeCountLimit,omitempty"`
	// workspace created by InferenceSet controller would use this label in resource.labelSelector
	// +required
	Selector *metav1.LabelSelector `json:"labelSelector"`
	// UpdateStrategy indicates the strategy to use to replace existing workspaces with new ones.
	// +optional
	// +kubebuilder:default:={"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":1}}
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	UpdateStrategy appsv1.StatefulSetUpdateStrategy `json:"updateStrategy,omitempty"`
}

// InferenceSetStatus defines the observed state of InferenceSet
type InferenceSetStatus struct {
	// Conditions report the current conditions of the InferenceSet.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// InferenceSet is the Schema for the InferenceSet API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=inferencesets,scope=Namespaced,categories=inferenceset,shortName={is,isets}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description=""
// +kubebuilder:printcolumn:name="ReadyReplicas",type="integer",JSONPath=".status.readyReplicas",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type InferenceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InferenceSetSpec   `json:"spec,omitempty"`
	Status InferenceSetStatus `json:"status,omitempty"`
}

// InferenceSetList contains a list of InferenceSet
// +kubebuilder:object:root=true
type InferenceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InferenceSet{}, &InferenceSetList{})
}
