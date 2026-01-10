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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PersistentVolumeConfig struct {
	// PersistentVolumeClaim specifies the PVC to use for persisting vector database data.
	PersistentVolumeClaim string `json:"persistentVolumeClaim"`
	// MountPath specifies where the volume should be mounted in the container.
	// Defaults to /mnt/data if not specified.
	// +optional
	MountPath string `json:"mountPath,omitempty"`
}

type StorageSpec struct {
	// PersistentVolume specifies PVC-based persistent storage configuration.
	// If not specified, an emptyDir will be used (data will be lost on pod restart).
	// +optional
	PersistentVolume *PersistentVolumeConfig `json:"persistentVolume,omitempty"`
}

type RemoteEmbeddingSpec struct {
	// URL points to a publicly available embedding service, such as OpenAI.
	URL string `json:"url"`
	// AccessSecret is the name of the secret that contains the service access token.
	// +optional
	AccessSecret string `json:"accessSecret,omitempty"`
}

type LocalEmbeddingSpec struct {
	// Image is the name of the containerized embedding model image.
	// +optional
	Image string `json:"image,omitempty"`
	// +optional
	ImagePullSecret string `json:"imagePullSecret,omitempty"`
	// ModelID is the ID of the embedding model hosted by huggingface, e.g., BAAI/bge-small-en-v1.5.
	// When this field is specified, the RAG engine will download the embedding model
	// from huggingface repository during startup. The embedding model will not persist in local storage.
	// Note that if Image is specified, ModelID should not be specified and vice versa.
	// +optional
	ModelID string `json:"modelID,omitempty"`
	// ModelAccessSecret is the name of the secret that contains the huggingface access token.
	// +optional
	ModelAccessSecret string `json:"modelAccessSecret,omitempty"`
}

type EmbeddingSpec struct {
	// Remote specifies how to generate embeddings for index data using a remote service.
	// Note that either Remote or Local needs to be specified, not both.
	// +optional
	Remote *RemoteEmbeddingSpec `json:"remote,omitempty"`
	// Local specifies how to generate embeddings for index data using a model run locally.
	// +optional
	Local *LocalEmbeddingSpec `json:"local,omitempty"`
}

type InferenceServiceSpec struct {
	// URL points to a running inference service endpoint which accepts http(s) payload.
	URL string `json:"url"`
	// AccessSecret is the name of the secret that contains the service access token.
	// +optional
	AccessSecret string `json:"accessSecret,omitempty"`
	// ContextWindowSize defines the combined maximum of input and output tokens that can be handled by the LLM in a single request.
	// This value is critical for accurately managing how much of the original query and supporting documents
	// (retrieved via RAG) can be included in the prompt without exceeding the model's input limit.
	//
	// It is used to determine how much space is available for retrieved documents after accounting for the query,
	// system prompts, formatting tokens, and any other fixed prompt components.
	//
	// Setting this value correctly is essential for ensuring that the RAG system does not truncate important
	// context or exceed model limits, which can lead to degraded response quality or inference errors.
	//
	// Must match the token limit of the LLM backend being used (e.g., 8096, 16384, 32768 tokens).
	ContextWindowSize int `json:"contextWindowSize"`
}

type RAGEngineSpec struct {
	// Compute specifies the dedicated GPU resource used by an embedding model running locally if required.
	// +optional
	Compute *ResourceSpec `json:"compute,omitempty"`
	// Storage specifies how to access the vector database used to save the embedding vectors.
	// If this field is not specified, by default, an in-memory vector DB will be used.
	// The data will not be persisted.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`
	// Embedding specifies whether the RAG engine generates embedding vectors using a remote service
	// or using a embedding model running locally.
	Embedding        *EmbeddingSpec        `json:"embedding"`
	InferenceService *InferenceServiceSpec `json:"inferenceService"`
}

// RAGEngineStatus defines the observed state of RAGEngine
type RAGEngineStatus struct {
	// WorkerNodes is the list of nodes chosen to run the workload based on the RAGEngine resource requirement.
	// +optional
	WorkerNodes []string `json:"workerNodes,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RAGEngine is the Schema for the ragengine API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=ragengines,scope=Namespaced,categories=ragengine,shortName=rag
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.compute.instanceType",description=""
// +kubebuilder:printcolumn:name="ResourceReady",type="string",JSONPath=".status.conditions[?(@.type==\"ResourceReady\")].status",description=""
// +kubebuilder:printcolumn:name="ServiceReady",type="string",JSONPath=".status.conditions[?(@.type==\"ServiceReady\")].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type RAGEngine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec *RAGEngineSpec `json:"spec,omitempty"`

	Status RAGEngineStatus `json:"status,omitempty"`
}

// RAGEngineList contains a list of RAGEngine
// +kubebuilder:object:root=true
type RAGEngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RAGEngine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RAGEngine{}, &RAGEngineList{})
}

// Hub marks this type as a conversion hub.
func (*RAGEngine) Hub() {}
