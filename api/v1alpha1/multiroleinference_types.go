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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MultiRoleInferenceRoleType defines the type of inference role.
type MultiRoleInferenceRoleType string

const (
	// MultiRoleInferenceRolePrefill is the prefill role for prompt processing.
	MultiRoleInferenceRolePrefill MultiRoleInferenceRoleType = "prefill"
	// MultiRoleInferenceRoleDecode is the decode role for token generation.
	MultiRoleInferenceRoleDecode MultiRoleInferenceRoleType = "decode"
)

// MultiRoleInferenceModelSpec defines the shared model configuration.
// The model is the core shared resource — all roles serve the same model
// with different runtime configurations.
type MultiRoleInferenceModelSpec struct {
	// Name is the model identifier (e.g., HuggingFace model ID).
	// This maps to the preset name used when generating child InferenceSets.
	// +required
	Name string `json:"name"`

	// ModelAccessSecret references a Secret containing credentials for model download
	// (e.g., HuggingFace token). Applied to all child workloads.
	// +optional
	ModelAccessSecret string `json:"modelAccessSecret,omitempty"`
}

// MultiRoleInferenceRoleSpec defines the configuration for a single inference role.
type MultiRoleInferenceRoleSpec struct {
	// Type is the role type. Supported values: prefill, decode.
	// +kubebuilder:validation:Enum=prefill;decode
	// +required
	Type MultiRoleInferenceRoleType `json:"type"`

	// Replicas is the number of workspaces (InferenceSet replicas) for this role.
	// Each replica maps to one Workspace → one StatefulSet.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// InstanceType specifies the GPU node SKU for this role.
	// Different roles may use different instance types (e.g., larger GPU for prefill).
	// +required
	InstanceType string `json:"instanceType"`

	// RuntimeConfig references a ConfigMap with role-specific vLLM runtime arguments.
	// These override or extend the default inference parameters for this role.
	// +optional
	RuntimeConfig string `json:"runtimeConfig,omitempty"`
}

// MultiRoleInferenceSpec defines the desired state of MultiRoleInference.
type MultiRoleInferenceSpec struct {
	// LabelSelector is propagated to generated child workloads (InferenceSets, Workspaces).
	// The InferencePool uses this selector (plus apps.kubernetes.io/pod-index: "0" for
	// Ray cluster support) to discover all prefill and decode workspaces.
	// +required
	// +kubebuilder:validation:XValidation:rule="(has(self.matchLabels) && size(self.matchLabels) > 0) || (has(self.matchExpressions) && size(self.matchExpressions) > 0)",message="labelSelector must have at least one matchLabels or matchExpressions entry"
	LabelSelector *metav1.LabelSelector `json:"labelSelector"`

	// Model defines the shared model configuration across all roles.
	// +required
	Model MultiRoleInferenceModelSpec `json:"model"`

	// EPPPluginsConfig references a ConfigMap containing custom EPP plugins configuration.
	// If not set, the controller auto-generates a standard P/D disaggregation plugin config
	// with prefill-filter, decode-filter, precise-prefix-cache-scorer, and load-aware-scorer.
	// +optional
	EPPPluginsConfig string `json:"eppPluginsConfig,omitempty"`

	// Roles defines the role topology of this inference service.
	// Exactly two roles are required: one prefill and one decode.
	// +required
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.exists(r, r.type == 'prefill') && self.exists(r, r.type == 'decode')",message="exactly one prefill and one decode role required"
	Roles []MultiRoleInferenceRoleSpec `json:"roles"`
}

// MultiRoleInferenceStatus defines the observed state of MultiRoleInference.
type MultiRoleInferenceStatus struct {
	// Conditions represent the latest available observations of the MultiRoleInference's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=multiroleinferences,scope=Namespaced,categories={kaito},shortName=mri
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model.name"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MultiRoleInference is the Schema for the multiroleinferences API.
// It orchestrates prefill/decode disaggregated inference by creating child InferenceSets,
// a shared InferencePool, and an EPP configured for P/D-aware request routing.
type MultiRoleInference struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultiRoleInferenceSpec   `json:"spec"`
	Status MultiRoleInferenceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MultiRoleInferenceList contains a list of MultiRoleInference.
type MultiRoleInferenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultiRoleInference `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultiRoleInference{}, &MultiRoleInferenceList{})
}
