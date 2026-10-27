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

package cachetest

import (
	"sort"
	"sync"

	"github.com/kaito-project/kaito/pkg/cache"
)

// MutationExpectation declares the provider-specific pod mutations expected for
// a single cache concern.
type MutationExpectation struct {
	Supported            bool
	ExpectEmpty          bool
	RequiredLabels       map[string]string
	RequiredEnvVars      []string
	RequiredVolumes      []string
	RequiredVolumeMounts []string
	Validate             func(m *cache.PodMutations) []error
}

// Expectations is a provider's self-declared conformance contract.
type Expectations struct {
	Provider          cache.ProviderName
	NewForConformance func() cache.Provider
	ModelWeights      MutationExpectation
	KVCache           MutationExpectation
}

// ForConcern returns the mutation expectation for the given cache concern.
func (e Expectations) ForConcern(concern cache.CacheConcern) MutationExpectation {
	switch concern {
	case cache.CacheConcernModelWeights:
		return e.ModelWeights
	case cache.CacheConcernKVCache:
		return e.KVCache
	default:
		return MutationExpectation{}
	}
}

var (
	expMu        sync.RWMutex
	expectations = map[cache.ProviderName]Expectations{}
)

// RegisterExpectations records a provider's conformance contract.
func RegisterExpectations(e Expectations) {
	expMu.Lock()
	defer expMu.Unlock()
	expectations[e.Provider] = e
}

// GetExpectations returns the registered expectations for a provider.
func GetExpectations(name cache.ProviderName) (Expectations, bool) {
	expMu.RLock()
	defer expMu.RUnlock()
	e, ok := expectations[name]
	return e, ok
}

// ListExpectations returns all registered provider expectations in name order.
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
