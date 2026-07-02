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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

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
	// Used to trigger webhook-based injection (e.g., dacs.azure.com/inject: "true").
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

// Provider defines the interface that cache implementations must satisfy.
// Each provider handles the specifics of its cache backend while KAITO's
// workspace controller interacts through this common contract.
type Provider interface {
	// Name returns the provider identifier (e.g., "dacs", "fluid").
	Name() string

	// IsAvailable reports whether the cache infrastructure is installed
	// and the provider can operate. cacheName identifies a specific cache
	// instance; if empty, checks for any cache infrastructure.
	IsAvailable(ctx context.Context, cacheName string) (bool, error)

	// IsReady reports whether the cache is warmed and ready to serve.
	// cacheName identifies a specific cache instance; if empty, checks any
	// available cache. Returns (ready, reason, error).
	IsReady(ctx context.Context, cacheName string) (bool, string, error)

	// PodMutations returns the pod-level changes needed for a specific cache
	// concern (ModelWeights or KVCache). cacheName identifies the target cache
	// instance; if empty, the provider uses its default. The provider should
	// only return mutations relevant to the requested concern.
	PodMutations(ctx context.Context, concern CacheConcern, workspace *kaitov1beta1.Workspace, modelName, modelRevision, cacheName string) (*PodMutations, error)

	// Cleanup invalidates cached data associated with a workspace.
	Cleanup(ctx context.Context, workspace *kaitov1beta1.Workspace, modelName string) error
}

// EventTarget is an optional interface providers can implement to return a
// runtime.Object for Kubernetes event emission (e.g., a Cache CR).
type EventTarget interface {
	EventObject() runtime.Object
}

// DefaultConfigProvider is an optional interface providers can implement to
// supply Helm-value defaults for a given concern. These defaults are merged
// with user-provided Config ConfigMap data (user overrides defaults).
type DefaultConfigProvider interface {
	DefaultConfig(concern string) map[string]string
}
