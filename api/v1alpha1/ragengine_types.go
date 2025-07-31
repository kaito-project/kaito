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

type StorageSpec struct {
	//TODO: add vendor specific APIs for accessing vector DB services here.
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
	// QueryServiceName is the name of the service which exposes the endpoint for accepting user queries to the
	// inference service. If not specified, a default service name will be created by the RAG engine.
	// +optional
	QueryServiceName string `json:"queryServiceName,omitempty"`
	// IndexServiceName is the name of the service which exposes the endpoint for user to input the index data
	// to generate embeddings. If not specified, a default service name will be created by the RAG engine.
	// +optional
	IndexServiceName string `json:"indexServiceName,omitempty"`
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
