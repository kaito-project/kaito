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
	"sort"
	"sync"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// The expectations registry is the backbone of provider-agnostic conformance
// testing. Every cache provider self-declares, via RegisterExpectations (typically
// in its package init), the pod mutations it is expected to produce for each cache
// concern. The conformance suite iterates the registered expectations (i.e. every
// discovered provider) and verifies each provider against its own contract.
//
// This makes provider testing automatic: adding a new provider and registering its
// Expectations is sufficient for the shared suite to discover and verify it — no
// edits to the core test are required. A provider that is registered without
// Expectations (or without a NewForConformance factory) fails the suite closed.

// MutationExpectation declares the provider-specific pod mutations expected for a
// single cache concern (model weights or KV cache). It is pure declarative data so
// it can live in production code without pulling in any test dependencies.
type MutationExpectation struct {
	// Supported indicates the provider handles this concern. When false, the
	// conformance suite skips concern-specific assertions for it.
	Supported bool

	// ExpectEmpty asserts the provider injects no mutations for this concern
	// (e.g. the noop provider). Mutually exclusive with the Required* fields.
	ExpectEmpty bool

	// RequiredLabels must all be present, with matching values, in the produced
	// pod template labels.
	RequiredLabels map[string]string

	// RequiredEnvVars lists env var names that must be present on the model container.
	RequiredEnvVars []string

	// RequiredVolumes lists volume names that must be present in the pod spec.
	RequiredVolumes []string

	// RequiredVolumeMounts lists volume mount names that must be present on the
	// model container.
	RequiredVolumeMounts []string

	// Validate is an optional deep validator for provider-specific values or
	// formats (e.g. a discovery URL shape or a specific env var value). It runs
	// against the raw PodMutations produced by PodMutations.
	Validate func(m *PodMutations) []error
}

// Expectations is a provider's self-declared conformance contract. Providers
// register their Expectations so the provider-agnostic conformance suite can
// automatically discover and verify any provider.
type Expectations struct {
	// Provider is the provider name these expectations apply to. Must match the
	// value returned by Provider.Name().
	Provider kaitov1beta1.CacheProvider

	// NewForConformance returns a Provider instance suitable for offline (no live
	// cluster) mutation conformance. It must not require a reachable cache backend,
	// because it is invoked by unit tests. Providers that talk to the API server in
	// PodMutations should wire a nil/fake client here.
	NewForConformance func() Provider

	// ModelWeights declares the expected mutations for the model weights concern.
	ModelWeights MutationExpectation

	// KVCache declares the expected mutations for the KV cache concern.
	KVCache MutationExpectation
}

// ForConcern returns the MutationExpectation for the given cache concern.
func (e Expectations) ForConcern(concern CacheConcern) MutationExpectation {
	switch concern {
	case CacheConcernModelWeights:
		return e.ModelWeights
	case CacheConcernKVCache:
		return e.KVCache
	default:
		return MutationExpectation{}
	}
}

var (
	expMu        sync.RWMutex
	expectations = map[kaitov1beta1.CacheProvider]Expectations{}
)

// RegisterExpectations records a provider's conformance contract. It should be
// called from each provider package's init so the expectations are registered
// whenever the provider package is imported.
func RegisterExpectations(e Expectations) {
	expMu.Lock()
	defer expMu.Unlock()
	expectations[e.Provider] = e
}

// GetExpectations returns the registered expectations for a provider.
func GetExpectations(name kaitov1beta1.CacheProvider) (Expectations, bool) {
	expMu.RLock()
	defer expMu.RUnlock()
	e, ok := expectations[name]
	return e, ok
}

// ListExpectations returns all registered provider expectations, sorted by
// provider name for deterministic iteration.
func ListExpectations() []Expectations {
	expMu.RLock()
	defer expMu.RUnlock()
	result := make([]Expectations, 0, len(expectations))
	for _, e := range expectations {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Provider < result[j].Provider
	})
	return result
}
