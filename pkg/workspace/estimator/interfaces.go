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

package estimator

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeProfile carries runtime serving parameters resolved by the caller
// before invoking the estimator.
type RuntimeProfile struct {
	// ContextSize is the model context window length (max-model-len).
	// A zero value signals that the estimator should apply its built-in default.
	ContextSize int
}

// ModelProfile identifies the model to be served.
type ModelProfile struct {
	// Name is the preset model name; an empty string means no preset inference.
	Name string
	// AccessToken is the pre-resolved access token for gated models (e.g. a HuggingFace API token).
	// Pass an empty string for public models that require no authentication.
	AccessToken string
}

// ResourceProfile describes the compute resources available for the workload.
type ResourceProfile struct {
	// InstanceType is the GPU SKU identifier (e.g. "Standard_NC4as_T4_v3").
	InstanceType string
	// RequestedNodeCount is the caller-preferred node count; 0 means unspecified.
	RequestedNodeCount int
	// LabelSelector is used in BYO (Bring Your Own) node scenarios to locate existing nodes.
	LabelSelector *metav1.LabelSelector
	// DisableNodeAutoProvisioning indicates BYO (Bring Your Own) mode: no new nodes will be
	// provisioned and the estimator must derive GPU config from existing ready nodes.
	DisableNodeAutoProvisioning bool
}

// NodeEstimateRequest holds all inputs needed to estimate the required node count.
type NodeEstimateRequest struct {
	// WorkspaceName is used for logging and diagnostics.
	WorkspaceName string
	// ModelProfile identifies the model preset and any access credentials.
	ModelProfile ModelProfile
	// ResourceProfile describes the compute resources for the workload.
	ResourceProfile ResourceProfile
	// RuntimeProfile carries pre-resolved serving parameters (e.g. context window size).
	RuntimeProfile RuntimeProfile
}

// NodesEstimator is an interface for estimating the number of nodes required for an inference workload.
type NodesEstimator interface {
	// Name a human-readable identifier for this estimator implementation.
	Name() string

	// EstimateNodeCount determines the minimum number of nodes required to serve the given model.
	// It inspects the model, resource, and runtime profiles in req to compute the estimate,
	// and may query the cluster via client to discover existing node capacity in BYO scenarios.
	// Returns the estimated node count or an error if the estimation cannot be performed.
	EstimateNodeCount(ctx context.Context, req NodeEstimateRequest, client client.Client) (int32, error)
}
