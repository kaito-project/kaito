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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=modelmirrors,scope=Cluster
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.source.modelID`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ModelMirror represents a cached copy of a model from a remote registry.
type ModelMirror struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ModelMirrorSpec   `json:"spec,omitempty"`
	Status            ModelMirrorStatus `json:"status,omitempty"`
}

type ModelMirrorSpec struct {
	// Mode selects how the model weights are made available:
	//   - "Managed" (default): the controller downloads the model to a PVC via a download Job.
	//   - "Static": the model weights already exist in a pre-existing (BYO) storage location;
	//     the controller creates no PVC and no download Job, and the weights location is
	//     reported in status.modelPath.
	// +kubebuilder:validation:Enum=Managed;Static
	// +kubebuilder:default=Managed
	// +optional
	Mode ModelMirrorMode `json:"mode,omitempty"`
	// Source describes where to download the model weights from. Required for a Managed
	// mirror; omit entirely for a Static mirror.
	// +optional
	Source *ModelMirrorSource `json:"source,omitempty"`
	// Storage describes the PVC to download the model weights into. Required for a Managed
	// mirror; omit entirely for a Static mirror.
	// +optional
	Storage *ModelMirrorStorage `json:"storage,omitempty"`
	// JobNamespace is the namespace where the PVC and download Job will be created;
	// omit entirely for static mirrors.
	// +optional
	JobNamespace string `json:"jobNamespace,omitempty"`
}

// ModelMirrorMode describes how a ModelMirror provisions the model weights.
type ModelMirrorMode string

const (
	// ModelMirrorModeManaged downloads the model to a PVC via a download Job.
	ModelMirrorModeManaged ModelMirrorMode = "Managed"
	// ModelMirrorModeStatic maps to an existing (BYO) storage location where the model
	// weights are already available. No PVC or download Job is created, the weights
	// location is reported in status.modelPath.
	ModelMirrorModeStatic ModelMirrorMode = "Static"
)

// Supported ModelMirror source registries.
const (
	RegistryHuggingFace = "huggingface"
)

// SupportedRegistries is the set of accepted ModelMirrorSource.Registry values.
var SupportedRegistries = []string{RegistryHuggingFace}

type ModelMirrorSource struct {
	// Registry is the source registry to download the model weights from.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=huggingface
	Registry string `json:"registry"`
	// ModelID is the model identifier (e.g. "Qwen/Qwen2.5-Coder-32B-Instruct").
	// +kubebuilder:validation:Required
	ModelID string `json:"modelID"`
	// AccessSecret references a secret containing authentication credentials.
	// +optional
	AccessSecret *corev1.ObjectReference `json:"accessSecret,omitempty"`
}

type ModelMirrorStorage struct {
	// Size is the PVC size for managed mirrors.
	// +kubebuilder:validation:Required
	Size string `json:"size"`
	// StorageClassName is the StorageClass to use for the PVC.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type ModelMirrorPhase string

const (
	ModelMirrorPhasePending ModelMirrorPhase = "Pending"
	ModelMirrorPhaseReady   ModelMirrorPhase = "Ready"
)

type ModelMirrorStatus struct {
	// +kubebuilder:validation:Enum=Pending;Ready
	Phase            ModelMirrorPhase   `json:"phase,omitempty"`
	ModelPath        string             `json:"modelPath,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
	FailureMessage   string             `json:"failureMessage,omitempty"`
	LastDownloadTime *metav1.Time       `json:"lastDownloadTime,omitempty"`
}

// +kubebuilder:object:root=true
type ModelMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelMirror{}, &ModelMirrorList{})
}
