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

	// E2EExempt opts the provider OUT of the online (in-cluster) e2e conformance
	// suite. It defaults to false, so every registered provider is exercised by the
	// shared, provider-agnostic e2e contract specs by default. A provider must
	// explicitly set this to true — e.g. the noop dummy, or a provider whose cache
	// backend cannot run in CI — to be excluded. Making e2e the safe default ensures
	// any future provider is tested unless it deliberately opts out.
	E2EExempt bool

	// ModelWeights declares the expected mutations for the model weights concern.
	ModelWeights MutationExpectation

	// KVCache declares the expected mutations for the KV cache concern.
	KVCache MutationExpectation

	// E2EScenarios are provider-specific end-to-end scenarios (e.g. backend
	// discovery, CR deletion, data-plane counters) that the e2e conformance suite
	// discovers and runs for this provider. Contract-level behaviour shared by all
	// providers is exercised by the parameterized agnostic specs instead; these are
	// only for behaviour unique to the provider.
	E2EScenarios []E2EScenario

	// CacheWarm returns the provider's cache warm/hit test configuration after
	// checking all prerequisites (env vars, patterns, etc.). Nil means the
	// provider does not support the cache-hit scenario. A non-nil result with
	// a non-empty SkipReason means the test should be skipped. All validation
	// logic lives in the provider — the e2e runner simply calls and acts on
	// the result.
	CacheWarm func() *CacheWarmConfig

	// E2EWorkspace returns the provider-specific workspace pod template the
	// shared e2e conformance specs need to run a real serving workload for this
	// provider (serving image, vLLM args, backend env, identity), after checking
	// the provider's e2e prerequisites. Nil means the provider relies on the
	// neutral skeleton the shared specs build (sufficient for a provider whose
	// cache engages without a special serving image or load path). A non-nil
	// result with a SkipReason means the provider's workspace-building specs are
	// skipped. Keeping all provider-specific env reads and pod shaping here frees
	// the shared e2e suite of provider details, so a new provider is exercised
	// end-to-end by registering its Expectations alone.
	E2EWorkspace func() *E2EWorkspaceConfig
}

// E2EWorkspaceConfig is returned by a provider's E2EWorkspace function. It supplies
// the provider-specific pod template the shared e2e conformance specs apply on top
// of the neutral workspace skeleton they build.
type E2EWorkspaceConfig struct {
	// SkipReason is non-empty when the provider's e2e prerequisites are unmet
	// (e.g. a required serving image or backend env var is not set). The shared
	// specs skip the provider's workspace-building tests with this message
	// instead of failing. This replaces suite-wide, provider-specific env gating.
	SkipReason string

	// Customizer fills in the provider's runtime specifics on the skeleton
	// workspace the shared specs build (serving image, vLLM args, backend env,
	// etc.). The specs own the provider-neutral scaffolding (metadata, resource
	// selector, cache spec, service account); the provider owns everything the
	// cache backend requires to engage. Must not be nil when SkipReason is empty.
	Customizer func(ws *kaitov1beta1.Workspace)
}

// CacheWarmConfig is returned by a provider's CacheWarm function. It carries
// everything the e2e runner needs to execute the warm/hit data-plane scenario.
// The runner orchestrates workspace lifecycle; the provider owns validation.
type CacheWarmConfig struct {
	// SkipReason is non-empty when the provider determined the test cannot
	// run (e.g. missing env vars). The e2e runner skips with this message.
	SkipReason string

	// Namespace overrides the default test namespace for cache-hit workspaces.
	// Providers that rely on workload identity federation should set this to a
	// fixed, pre-configured namespace (read from an env var) so that the SA
	// federation works across randomly-named e2e namespaces.
	// Empty means use the default e2e test namespace.
	Namespace string

	// WorkspaceCustomizer applies provider-specific overrides to a workspace
	// before it is created (e.g. model URL, storage account, vLLM args).
	// Nil means no customization is needed.
	WorkspaceCustomizer func(ws *kaitov1beta1.Workspace)

	// ValidatePreWarm is called after the first (cold) model load reaches
	// InferenceReady. The provider validates warm-up behaviour (e.g. log
	// patterns indicating blob reads). Nil means no pre-warm validation.
	ValidatePreWarm func(h E2EHarness, ws *kaitov1beta1.Workspace) error

	// ValidatePostWarm is called after the second (warm) model load reaches
	// InferenceReady. The provider validates cache-hit behaviour (e.g. log
	// patterns indicating remote cache reads). Must not be nil.
	ValidatePostWarm func(h E2EHarness, ws *kaitov1beta1.Workspace) error
}

// RunsE2E reports whether the provider participates in the online (in-cluster)
// e2e conformance suite. It is the inverse of E2EExempt, so e2e coverage is the
// default for every registered provider.
func (e Expectations) RunsE2E() bool { return !e.E2EExempt }

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
