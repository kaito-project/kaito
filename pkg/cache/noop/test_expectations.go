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

package noop

import (
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

// init registers the noop provider's conformance expectations so the
// provider-agnostic conformance suite automatically discovers and verifies it
// whenever this package is imported. The noop provider injects nothing, so both
// concerns are supported but expected to produce empty mutations.
func init() {
	cache.RegisterExpectations(cache.Expectations{
		Provider:          kaitov1beta1.CacheProvider(ProviderName),
		NewForConformance: func() cache.Provider { return NewProvider() },
		// The noop provider injects nothing and has no serving backend, so the
		// online (in-cluster) e2e specs — which build a real inference workload —
		// have nothing meaningful to exercise for it. Opt out of e2e; the offline
		// conformance and unit tests still verify its empty-mutation behavior.
		E2EExempt: true,
		ModelWeights: cache.MutationExpectation{
			Supported:   true,
			ExpectEmpty: true,
		},
		KVCache: cache.MutationExpectation{
			Supported:   true,
			ExpectEmpty: true,
		},
	})
}
