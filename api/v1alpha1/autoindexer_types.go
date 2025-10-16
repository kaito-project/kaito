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

// AutoIndexer is the Schema for the autoindexer API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=autoindexers,scope=Namespaced,categories=autoindexer,shortName=ragai
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="ResourceReady",type="string",JSONPath=".status.conditions[?(@.type==\"ResourceReady\")].status",description=""
// +kubebuilder:printcolumn:name="Scheduled",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerScheduled\")].status",description=""
// +kubebuilder:printcolumn:name="Indexing",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerIndexing\")].status",description=""
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.indexingPhase",description=""
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerError\")].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type AutoIndexer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AutoIndexerSpec `json:"spec,omitempty"`

	Status AutoIndexerStatus `json:"status,omitempty"`
}

// AutoIndexerSpec defines the desired state of AutoIndexer
type AutoIndexerSpec struct {

	// RAGEngine references the name RAGEngine resource to use for indexing.
	// The RAGEngine must be in the same namespace as the AutoIndexer.
	// +kubebuilder:validation:Required
	RAGEngine string `json:"ragEngine"`

	// IndexName is the name of the index where documents will be stored
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9\-]*[a-z0-9]$`
	IndexName string `json:"indexName"`

	// DataSource defines where to retrieve documents for indexing
	// +kubebuilder:validation:Required
	DataSource DataSourceSpec `json:"dataSource"`

	// Credentials for private repositories
	// +optional
	Credentials *CredentialsSpec `json:"credentials,omitempty"`

	// Schedule defines when the indexing should run (cron format)
	// +optional
	// +kubebuilder:validation:Pattern=`^(@(annually|yearly|monthly|weekly|daily|hourly|reboot))|(@every (\d+(ns|us|µs|ms|s|m|h))+)|((((\d+,)+\d+|(\d+(\/|-)\d+)|\d+|\*) ?){5,7})$`
	Schedule *string `json:"schedule,omitempty"`

	// Suspend can be set to true to suspend the indexing schedule
	// This will also suspend any drift detection for data sources
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// DataSourceSpec defines the source of documents to be indexed
type DataSourceSpec struct {
	// Type specifies the data source type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Git;Static
	Type DataSourceType `json:"type"`

	// Git defines configuration for Git repository data sources
	// +optional
	Git *GitDataSourceSpec `json:"git,omitempty"`

	// Static defines configuration for static data sources
	// +optional
	Static *StaticDataSourceSpec `json:"static,omitempty"`
}

// DataSourceType defines the supported data source types
// +kubebuilder:validation:Enum=Git;Static
type DataSourceType string

const (
	DataSourceTypeGitHub DataSourceType = "Git"
	DataSourceTypeStatic DataSourceType = "Static"
)

// GitHubDataSourceSpec defines GitHub repository configuration
type GitDataSourceSpec struct {
	// Repository to index. If the repository is not public and a token is needed for access,
	// the access token can be stored in a secret and loaded with the SecretRef in the credential spec
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Branch to checkout (default: main)
	// +kubebuilder:validation:Required
	Branch string `json:"branch"`

	// Commit SHA to checkout. If included, only this commit will be put into the index
	// +optional
	Commit *string `json:"commit,omitempty"`

	// Specific paths to index within the repository.
	// Can be directories, specific files, or specific extension types: /src, main.py, *.go
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Paths to exclude from indexing. ExcludePaths takes priority over Paths.
	// Can be directories, specific files, or specific extension types: /src, main.py, *.go
	// +optional
	ExcludePaths []string `json:"excludePaths,omitempty"`
}

// APIDataSourceSpec defines REST API configuration
type StaticDataSourceSpec struct {
	// URLs that should point to individual text encoding (UTF-8, UTF-8-SIG, Latin1, etc) or pdf files.
	// If an access token is needed for the URL's, the access token can be stored in a secret
	// and loaded with the SecretRef in the credential spec
	// +kubebuilder:validation:Required
	URLs []string `json:"urls"`
}

// CredentialsSpec defines authentication credentials
type CredentialsSpec struct {
	// Type specifies the credential type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=SecretRef
	Type CredentialType `json:"type"`

	// Secret reference containing credentials
	// +optional
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
}

// CredentialType defines the supported credential types
// +kubebuilder:validation:Enum=SecretRef
type CredentialType string

const (
	CredentialTypeSecretRef CredentialType = "SecretRef"
)

// SecretKeyRef references a key in a Secret
type SecretKeyRef struct {
	// Secret name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key within the secret
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// AutoIndexerStatus defines the observed state of AutoIndexer
type AutoIndexerStatus struct {
	// LastIndexingTimestamp is the timestamp of the end of the last successful indexing
	// +optional
	LastIndexingTimestamp *metav1.Time `json:"lastIndexingTimestamp,omitempty"`

	// LastCommit is the last processed commit hash for Git sources
	// +optional
	LastIndexedCommit *string `json:"lastIndexedCommit,omitempty"`

	// LastRunDurationSeconds is the duration of the last indexer run in seconds
	// +optional
	LastIndexingDurationSeconds int32 `json:"lastIndexingDurationSeconds,omitempty"`

	// IndexingPhase represents the current phase of the AutoIndexer
	// +optional
	// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
	IndexingPhase AutoIndexerPhase `json:"indexingPhase,omitempty"`

	// SuccessfulIndexingCount tracks successful indexing runs
	SuccessfulIndexingCount int32 `json:"successfulIndexingCount"`

	// ErrorIndexingCount tracks failed indexing runs
	ErrorIndexingCount int32 `json:"errorIndexingCount"`

	// NumOfDocumentInIndex is the count of documents in the index after the latest run managed by this autoindexer instance
	NumOfDocumentInIndex int32 `json:"numOfDocumentInIndex"`

	// NextScheduledIndexing shows when the next indexing is scheduled
	// +optional
	NextScheduledIndexing *metav1.Time `json:"nextScheduledIndexing,omitempty"`

	// observedGeneration represents the observed .metadata.generation of the AutoIndexer
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current service state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AutoIndexerPhase defines the current phase of the AutoIndexer
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
type AutoIndexerPhase string

const (
	AutoIndexerPhasePending   AutoIndexerPhase = "Pending"
	AutoIndexerPhaseRunning   AutoIndexerPhase = "Running"
	AutoIndexerPhaseCompleted AutoIndexerPhase = "Completed"
	AutoIndexerPhaseFailed    AutoIndexerPhase = "Failed"
	AutoIndexerPhaseRetrying  AutoIndexerPhase = "Retrying"
	AutoIndexerPhaseUnknown   AutoIndexerPhase = "Unknown"
)

//+kubebuilder:object:root=true

// AutoIndexerList contains a list of AutoIndexer
type AutoIndexerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AutoIndexer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AutoIndexer{}, &AutoIndexerList{})
}
