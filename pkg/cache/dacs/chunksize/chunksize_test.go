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

package chunksize

import "testing"

func TestRecommended(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantValue int
		wantOK    bool
	}{
		{"moe model recommends modal 3 MiB", "qwen3-coder-30b-a3b-instruct", 3145728, true},
		{"dense 7b clamps to 32 MiB", "qwen2.5-coder-7b-instruct", 33554432, true},
		{"dense 32b clamps to 32 MiB", "qwen2.5-coder-32b-instruct", 33554432, true},
		{"gpt-oss-20b raised to 2 MiB floor", "gpt-oss-20b", 2097152, true},
		{"gpt-oss-120b clamps to 32 MiB", "gpt-oss-120b", 33554432, true},
		{"deepseek-r1 modal 14 MiB", "deepseek-r1-0528", 14680064, true},
		{"deepseek-v3 modal 14 MiB", "deepseek-v3-0324", 14680064, true},

		// The runtime model name is the lowercased HF repo basename, which for
		// these carries a version/date suffix — the map is keyed by that.
		{"mistral runtime basename", "mistral-7b-v0.3", 33554432, true},
		{"ministral runtime basename", "ministral-3-8b-instruct-2512", 2097152, true},
		// Short preset names still resolve via the emitted alias.
		{"mistral preset alias", "mistral-7b", 33554432, true},
		{"ministral preset alias", "ministral-3-8b-instruct", 2097152, true},

		// Recommended normalizes: full HF ids and mixed case resolve too.
		{"full HF id normalizes", "microsoft/phi-4", 33554432, true},
		{"mixed-case HF id normalizes", "Qwen/Qwen3-Coder-30B-A3B-Instruct", 3145728, true},

		{"unknown model", "acme/does-not-exist", 0, false},
		{"empty model", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Recommended(tt.model)
			if ok != tt.wantOK {
				t.Fatalf("Recommended(%q) ok = %v, want %v", tt.model, ok, tt.wantOK)
			}
			if got != tt.wantValue {
				t.Errorf("Recommended(%q) = %d, want %d", tt.model, got, tt.wantValue)
			}
		})
	}
}

// TestRecommendedWithinBounds guards the generator invariants that every
// recommendation is within the [2 MiB, 32 MiB] chunk range.
func TestRecommendedWithinBounds(t *testing.T) {
	const (
		minFloor = 2 * 1024 * 1024
		maxCap   = 32 * 1024 * 1024
	)
	for model, size := range recommendedChunkSizes {
		if size < minFloor {
			t.Errorf("%s: recommendation %d below 2 MiB floor %d", model, size, minFloor)
		}
		if size > maxCap {
			t.Errorf("%s: recommendation %d exceeds 32 MiB cap %d", model, size, maxCap)
		}
	}
}
