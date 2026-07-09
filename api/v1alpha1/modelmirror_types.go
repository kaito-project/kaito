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
	// +kubebuilder:validation:Required
	Source ModelMirrorSource `json:"source"`
	// +kubebuilder:validation:Required
	Storage ModelMirrorStorage `json:"storage"`
	// JobNamespace is the namespace where the PVC and download Job will be created.
	// Empty for stream-only sources that create no PVC or Job.
	// +optional
	JobNamespace string `json:"jobNamespace,omitempty"`
}

type ModelMirrorSource struct {
	// Registry is the source registry type. "huggingface" mirrors the model to a PVC;
	// "azureml" streams directly from a pre-existing blob (no PVC, no download).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=huggingface;azureml
	Registry string `json:"registry"`
	// ModelID is the model identifier (e.g. "Qwen/Qwen2.5-Coder-32B-Instruct").
	// +kubebuilder:validation:Required
	ModelID string `json:"modelID"`
	// AccessSecret references a secret containing authentication credentials.
	// +optional
	AccessSecret *corev1.ObjectReference `json:"accessSecret,omitempty"`
}

type ModelMirrorStorage struct {
	// Size is the requested model storage: the PVC size for mirror (huggingface)
	// sources, or informational for stream-only (azureml) sources that create no PVC.
	// +kubebuilder:validation:Required
	Size string `json:"size"`
	// StorageClassName is the StorageClass to use for the PVC. Nil for stream-only
	// sources that create no PVC.
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
