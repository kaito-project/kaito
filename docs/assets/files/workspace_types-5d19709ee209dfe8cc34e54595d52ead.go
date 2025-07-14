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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ModelImageAccessModePublic  ModelImageAccessMode = "public"
	ModelImageAccessModePrivate ModelImageAccessMode = "private"
)

// ResourceSpec describes the resource requirement of running the workload.
// If the number of nodes in the cluster that meet the InstanceType and
// LabelSelector requirements is small than the Count, controller
// will provision new nodes before deploying the workload.
// The final list of nodes used to run the workload is presented in workspace Status.
type ResourceSpec struct {
	// Count is the required number of GPU nodes.
	// +optional
	// +kubebuilder:default:=1
	Count *int `json:"count,omitempty"`

	// InstanceType specifies the GPU node SKU.
	// This field defaults to "Standard_NC24ads_A100_v4" if not specified.
	// +optional
	// +kubebuilder:default:="Standard_NC24ads_A100_v4"
	InstanceType string `json:"instanceType,omitempty"`

	// LabelSelector specifies the required labels for the GPU nodes.
	LabelSelector *metav1.LabelSelector `json:"labelSelector"`

	// PreferredNodes is an optional node list specified by the user.
	// If a node in the list does not have the required labels, it will be ignored.
	// +optional
	PreferredNodes []string `json:"preferredNodes,omitempty"`
}

type ModelName string

// +kubebuilder:validation:Enum=public;private
type ModelImageAccessMode string

type PresetMeta struct {
	// Name of the supported models with preset configurations.
	Name ModelName `json:"name"`
	// AccessMode specifies whether the containerized model image is accessible via public registry
	// or private registry. This field defaults to "public" if not specified.
	// If this field is "private", user needs to provide the private image information in PresetOptions.
	// +kubebuilder:default:="public"
	// +optional
	AccessMode ModelImageAccessMode `json:"accessMode,omitempty"`
}

type PresetOptions struct {
	// Image is the name of the containerized model image.
	// +optional
	Image string `json:"image,omitempty"`
	// ImagePullSecrets is a list of secret names in the same namespace used for pulling the model image.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`
}

// PresetSpec provides the information for rendering preset configurations to run the model inference service.
type PresetSpec struct {
	PresetMeta `json:",inline"`
	// +optional
	PresetOptions `json:"presetOptions,omitempty"`
}

type InferenceSpec struct {
	// Preset describes the base model that will be deployed with preset configurations.
	// +optional
	Preset *PresetSpec `json:"preset,omitempty"`
	// Template specifies the Pod template used to run the inference service. Users can specify custom Pod settings
	// if the preset configurations cannot meet the requirements. Note that if Preset is specified, Template should not
	// be specified and vice versa.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Template *v1.PodTemplateSpec `json:"template,omitempty"`
	// Config specifies the name of a custom ConfigMap that contains inference arguments.
	// If specified, the ConfigMap must be in the same namespace as the Workspace custom resource.
	// +optional
	Config string `json:"config,omitempty"`
	// Adapters are integrated into the base model for inference.
	// Users can specify multiple adapters for the model and the respective weight of using each of them.
	// +optional
	Adapters []AdapterSpec `json:"adapters,omitempty"`
}

type AdapterSpec struct {
	// Source describes where to obtain the adapter data.
	// +optional
	Source *DataSource `json:"source,omitempty"`
	// Strength specifies the default multiplier for applying the adapter weights to the raw model weights.
	// It is usually a float number between 0 and 1. It is defined as a string type to be language agnostic.
	// +optional
	Strength *string `json:"strength,omitempty"`
}

type DataSource struct {
	// The name of the dataset. The same name will be used as a container name.
	// It must be a valid DNS subdomain value,
	Name string `json:"name,omitempty"`
	// URLs specifies the links to the public data sources. E.g., files in a public github repository.
	// +optional
	URLs []string `json:"urls,omitempty"`
	// The mounted volume that contains the data.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Volume *v1.VolumeSource `json:"volumeSource,omitempty"`
	// The name of the image that contains the source data. The assumption is that the source data locates in the
	// `data` directory in the image.
	// +optional
	Image string `json:"image,omitempty"`
	// ImagePullSecrets is a list of secret names in the same namespace used for pulling the data image.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`
}

type DataDestination struct {
	// The mounted volume that is used to save the output data.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Volume *v1.VolumeSource `json:"volumeSource,omitempty"`
	// Name of the image where the output data is pushed to.
	// +optional
	Image string `json:"image,omitempty"`
	// ImagePushSecret is the name of the secret in the same namespace that contains the authentication
	// information that is needed for running `docker push`.
	// +optional
	ImagePushSecret string `json:"imagePushSecret,omitempty"`
}

type TuningMethod string

const (
	TuningMethodLora  TuningMethod = "lora"
	TuningMethodQLora TuningMethod = "qlora"
)

type TuningSpec struct {
	// Preset describes which model to load for tuning.
	// +optional
	Preset *PresetSpec `json:"preset,omitempty"`
	// Method specifies the Parameter-Efficient Fine-Tuning(PEFT) method, such as lora, qlora, used for the tuning.
	// +optional
	Method TuningMethod `json:"method,omitempty"`
	// Config specifies the name of a custom ConfigMap that contains tuning arguments.
	// If specified, the ConfigMap must be in the same namespace as the Workspace custom resource.
	// If not specified, a default Config is used based on the specified tuning method.
	// +optional
	Config string `json:"config,omitempty"`
	// Input describes the input used by the tuning method.
	Input *DataSource `json:"input"`
	// Output specified where to store the tuning output.
	Output *DataDestination `json:"output"`
}

// WorkspaceStatus defines the observed state of Workspace
type WorkspaceStatus struct {
	// WorkerNodes is the list of nodes chosen to run the workload based on the workspace resource requirement.
	// +optional
	WorkerNodes []string `json:"workerNodes,omitempty"`

	// Conditions report the current conditions of the workspace.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Workspace is the Schema for the workspaces API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=workspaces,scope=Namespaced,categories=workspace,shortName={wk,wks}
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".resource.instanceType",description=""
// +kubebuilder:printcolumn:name="ResourceReady",type="string",JSONPath=".status.conditions[?(@.type==\"ResourceReady\")].status",description=""
// +kubebuilder:printcolumn:name="InferenceReady",type="string",JSONPath=".status.conditions[?(@.type==\"InferenceReady\")].status",description=""
// +kubebuilder:printcolumn:name="JobStarted",type="string",JSONPath=".status.conditions[?(@.type==\"JobStarted\")].status",description=""
// +kubebuilder:printcolumn:name="WorkspaceSucceeded",type="string",JSONPath=".status.conditions[?(@.type==\"WorkspaceSucceeded\")].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Resource  ResourceSpec    `json:"resource,omitempty"`
	Inference *InferenceSpec  `json:"inference,omitempty"`
	Tuning    *TuningSpec     `json:"tuning,omitempty"`
	Status    WorkspaceStatus `json:"status,omitempty"`
}

// WorkspaceList contains a list of Workspace
// +kubebuilder:object:root=true
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}
