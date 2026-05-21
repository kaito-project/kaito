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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

type InferenceSetResourceSpec struct {
	// InstanceType specifies the GPU node SKU.
	// +required
	InstanceType string `json:"instanceType"`
}

// InferenceSetTemplate defines the template for creating InferenceSet instances.
type InferenceSetTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Resource  InferenceSetResourceSpec   `json:"resource"`
	Inference kaitov1beta1.InferenceSpec `json:"inference"`
}

// AutoUpgradePolicy configures automatic base image upgrade behavior.
type AutoUpgradePolicy struct {
	// Enabled controls whether the controller automatically upgrades
	// Workspace replicas when a newer base image version is detected
	// after a controller upgrade.
	// +optional
	// +kubebuilder:default:=false
	Enabled bool `json:"enabled"`

	// MaintenanceWindow restricts when upgrades may be applied.
	// If not specified, upgrades may be applied at any time.
	// +optional
	MaintenanceWindow *MaintenanceWindow `json:"maintenanceWindow,omitempty"`
}

// MaintenanceWindow restricts when auto-upgrades may be applied.
// The controller will only begin upgrading Workspaces when the current time
// falls within the specified window.
type MaintenanceWindow struct {
	// Schedule is a cron expression (5-field, UTC) defining when upgrades
	// are permitted to start. The window opens at each cron tick and stays
	// open for Duration.
	// Example: "0 2 * * 6" = every Saturday at 02:00 UTC.
	// +required
	Schedule string `json:"schedule"`

	// Duration specifies how long the maintenance window stays open after
	// each cron tick. If a rollout is still in progress when the window
	// closes, the in-progress Workspace upgrade is allowed to complete
	// (the controller will not start upgrading the next Workspace until the
	// next window opens).
	// Defaults to 4h.
	// +optional
	// +kubebuilder:default:="4h"
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// AutoUpgradeStatus reports the observed state of automatic base image upgrades.
type AutoUpgradeStatus struct {
	// NumDriftedWorkspaces is the number of Workspaces whose base image version
	// differs from the controller's embedded version. Nil when autoUpgrade is
	// disabled; 0 means all Workspaces are up-to-date.
	// +optional
	NumDriftedWorkspaces *int `json:"numDriftedWorkspaces,omitempty"`
	// LastSuccessfulUpgradeTime is the timestamp of the last Workspace that
	// successfully completed an auto-upgrade.
	// +optional
	LastSuccessfulUpgradeTime *metav1.Time `json:"lastSuccessfulUpgradeTime,omitempty"`
}

// InferenceSetSpec defines the desired state of InferenceSet
type InferenceSetSpec struct {
	// Template is the template used to create the InferenceSet.
	Template InferenceSetTemplate `json:"template"`
	// Replicas is the desired number of workspaces to be created.
	// +optional
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`
	// NodeCountLimit is the maximum number of GPU nodes that can be created for the InferenceSet.
	// If not specified, there is no limit on the number of GPU nodes that can be created.
	// +optional
	NodeCountLimit int `json:"nodeCountLimit,omitempty"`
	// workspace created by InferenceSet controller would use this label in resource.labelSelector
	// +required
	Selector *metav1.LabelSelector `json:"labelSelector"`
	// UpdateStrategy indicates the strategy to use to replace existing workspaces with new ones.
	// +optional
	// +kubebuilder:default:={"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":1}}
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	UpdateStrategy appsv1.StatefulSetUpdateStrategy `json:"updateStrategy,omitempty"`
	// AutoUpgrade configures automatic base image upgrade behavior.
	// When enabled, the controller detects base image version mismatches
	// after a controller upgrade and performs in-place rolling updates of
	// Workspace StatefulSets.
	// +optional
	AutoUpgrade *AutoUpgradePolicy `json:"autoUpgrade,omitempty"`
}

// Metric holds an aggregated benchmark measurement across workspace replicas.
type Metric struct {
	// Description describes the benchmark type and load pattern, e.g. "stress/high-concurrency".
	Description string `json:"description"`
	// Value is the aggregated metric value, formatted as a string.
	Value string `json:"value"`
	// Unit is the unit of the metric value (e.g. "tokens/min").
	// +optional
	Unit string `json:"unit,omitempty"`
}

// Performance holds aggregated performance characteristics across all workspace replicas,
// keyed by metric name (e.g. "aggregatedPeakTokensPerMinute").
type Performance struct {
	// Metrics is a map of metric name to Metric.
	// +optional
	Metrics map[string]Metric `json:"metrics,omitempty"`
}

// InferenceSetStatus defines the observed state of InferenceSet
type InferenceSetStatus struct {
	// Replicas is the total number of workspaces created by the InferenceSet.
	Replicas int `json:"replicas,omitempty"`
	// ReadyReplicas is the number of workspaces that are in ready state.
	ReadyReplicas int `json:"readyReplicas,omitempty"`
	// Selector is used to select the pods that provide metrics for making scaling action decisions.
	// This field must be set when HPA and VPA is used for scaling.
	Selector string `json:"selector,omitempty"`
	// Conditions report the current conditions of the InferenceSet.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Performance holds aggregated performance characteristics across all workspace replicas.
	// +optional
	Performance *Performance `json:"performance,omitempty"`
	// AutoUpgrade reports the observed state of automatic base image upgrades.
	// +optional
	AutoUpgrade *AutoUpgradeStatus `json:"autoUpgrade,omitempty"`
}

// InferenceSet is the Schema for the InferenceSet API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=inferencesets,scope=Namespaced,categories=inferenceset,shortName={is,isets}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description=""
// +kubebuilder:printcolumn:name="ReadyReplicas",type="integer",JSONPath=".status.readyReplicas",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type InferenceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of InferenceSet
	// +required
	Spec InferenceSetSpec `json:"spec"`

	// Status defines the observed state of InferenceSet
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
