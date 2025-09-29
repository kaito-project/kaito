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

	// RAGEngineRef references the RAGEngine resource to use for indexing
	// +kubebuilder:validation:Required
	RAGEngineRef RAGEngineReference `json:"ragEngineRef"`

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
	Schedule *string `json:"schedule"`

	// RetryPolicy defines how failed indexing jobs should be retried
	// +optional
	RetryPolicy *RetryPolicySpec `json:"retryPolicy,omitempty"`

	// Suspend can be set to true to suspend the indexing schedule
	// This will also suspend any drift detection for data sources
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// RAGEngineReference defines a reference to a ragengine object
type RAGEngineReference struct {
	// Name defines the ragengine name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace defines the namespace of the ragengine
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// DataSourceSpec defines the source of documents to be indexed
type DataSourceSpec struct {
	// Type specifies the data source type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Git;Static
	Type DataSourceType `json:"type"`

	// GitHub defines configuration for GitHub repository data sources
	// +optional
	Git *GitDataSourceSpec `json:"gitHub,omitempty"`

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
	// Repository URL
	// +kubebuilder:validation:Required
	RepositoryURL string `json:"repositoryURL"`

	// Branch to checkout (default: main)
	// +optional
	Branch string `json:"branch,omitempty"`

	// Commit SHA to checkout (optional)
	// +optional
	Commit string `json:"commit,omitempty"`

	// Specific paths to index within the repository
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Paths to exclude from indexing
	// +optional
	ExcludePaths []string `json:"excludePaths,omitempty"`
}

// APIDataSourceSpec defines REST API configuration
type StaticDataSourceSpec struct {
	// data endpoint URLs
	// +kubebuilder:validation:Required
	Endpoints []string `json:"endpoints"`
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

// RetryPolicySpec defines retry behavior for failed operations
type RetryPolicySpec struct {
	// Maximum number of retries applied to failed indexing jobs
	// Default is 3
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	// +optional
	MaxRetries int32 `json:"maxRetries,omitempty"`
}

// AutoIndexerStatus defines the observed state of AutoIndexer
type AutoIndexerStatus struct {
	// LastIndexed timestamp of the last successful indexing
	// +optional
	LastIndexed *metav1.Time `json:"lastIndexed,omitempty"`

	// LastCommit is the last processed commit hash for Git sources
	// +optional
	LastCommit string `json:"lastCommit,omitempty"`

	// LastRunDocuments indicates the number of documents indexed in the last run
	// +optional
	LastRunDocuments int32 `json:"lastRunDocuments,omitempty"`

	// Phase represents the current phase of the AutoIndexer
	// +optional
	// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
	Phase AutoIndexerPhase `json:"phase,omitempty"`

	// SuccessfulRunCount tracks successful indexing runs
	// +optional
	SuccessfulRunCount int32 `json:"successfulRunCount,omitempty"`

	// ErrorRunCount tracks failed indexing runs
	// +optional
	ErrorRunCount int32 `json:"errorRunCount,omitempty"`

	// Number of documents processed in the last run
	// +optional
	DocumentsProcessed int32 `json:"documentsProcessed,omitempty"`

	// NextScheduledRun shows when the next indexing is scheduled
	// +optional
	NextScheduledRun *metav1.Time `json:"nextScheduledRun,omitempty"`

	// Errors from the last indexing operation
	// +optional
	Errors []string `json:"errors,omitempty"`

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
