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

package cache

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// CacheConcern identifies which cache concern a provider is being asked about.
type CacheConcern string

const (
	CacheConcernModelWeights CacheConcern = "ModelWeights"
	CacheConcernKVCache      CacheConcern = "KVCache"
)

// PodMutations describes all pod-level changes needed to enable cache access.
// Supports both env-var-based (e.g., storage interception libraries) and
// mount-based (e.g., FUSE, PVC) cache integrations.
type PodMutations struct {
	// Labels to add to the pod template metadata.
	// Used to trigger webhook-based injection (e.g., tachyon.azure.com/inject: "true").
	Labels map[string]string
	// EnvVars to inject into model containers.
	EnvVars []corev1.EnvVar
	// Volumes to add to the pod spec.
	Volumes []corev1.Volume
	// VolumeMounts to add to model containers.
	VolumeMounts []corev1.VolumeMount
	// InitContainers to prepend to the pod.
	InitContainers []corev1.Container
}

// PrewarmRequest describes a model to be prewarmed into the cache.
// Decoupled from Workspace so it can be triggered by either a Workspace
// controller (on deploy) or a future Model CR controller (on creation).
type PrewarmRequest struct {
	// ModelName is the model identifier (e.g., "microsoft/phi-4").
	ModelName string
	// ModelRevision is the model revision (commit hash or "main").
	ModelRevision string
	// ModelSource is the registry or repository path for model weights.
	ModelSource string
	// ModelAccessSecret is the name of the K8s Secret containing the HF token.
	ModelAccessSecret string
	// ModelFiles optionally lists specific weight files to prewarm.
	// If empty, the provider should prewarm all files for the model.
	ModelFiles []string
}

// PrewarmPhase represents the status of a prewarm operation.
type PrewarmPhase string

const (
	PrewarmPhasePending   PrewarmPhase = "Pending"
	PrewarmPhaseRunning   PrewarmPhase = "Running"
	PrewarmPhaseSucceeded PrewarmPhase = "Succeeded"
	PrewarmPhaseFailed    PrewarmPhase = "Failed"
	PrewarmPhaseUnknown   PrewarmPhase = "Unknown"
)

// Provider defines the interface that cache implementations must satisfy.
// Each provider handles the specifics of its cache backend while KAITO's
// workspace controller interacts through this common contract.
type Provider interface {
	// Name returns the provider identifier (e.g., "tachyon", "fluid").
	Name() string

	// IsAvailable reports whether the cache infrastructure is installed
	// and the provider can operate (e.g., CRD exists, operator running).
	IsAvailable(ctx context.Context) (bool, error)

	// IsReady reports whether the cache is warmed and ready to serve.
	// Returns (ready, reason, error).
	IsReady(ctx context.Context) (bool, string, error)

	// PodMutations returns the pod-level changes needed for a specific cache
	// concern (ModelWeights or KVCache). The provider should only return
	// mutations relevant to the requested concern.
	PodMutations(ctx context.Context, concern CacheConcern, workspace *kaitov1beta1.Workspace, modelName, modelRevision string) (*PodMutations, error)

	// Prewarm triggers cache population for the specified model.
	// Can be called by the workspace controller (on deploy with prewarmOnDeploy)
	// or by a future Model CR controller (on model registration).
	// Returns immediately; warming is asynchronous.
	Prewarm(ctx context.Context, req PrewarmRequest) error

	// Cleanup invalidates cached data for the specified model.
	Cleanup(ctx context.Context, req PrewarmRequest) error
}

// PrewarmJobBuilder is an optional interface that providers can implement
// to supply a Kubernetes Job spec for prewarming. The reconciler handles
// Job creation and lifecycle management, keeping providers declarative.
type PrewarmJobBuilder interface {
	BuildPrewarmJob(req PrewarmRequest, namespace string) *batchv1.Job
}

