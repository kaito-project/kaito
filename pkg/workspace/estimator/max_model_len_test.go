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
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestComputeMaxModelLen(t *testing.T) {
	gib := func(s string) int64 {
		q := resource.MustParse(s)
		return q.Value()
	}

	tests := []struct {
		name        string
		input       MaxModelLenInput
		expected    int
		description string
	}{
		{
			name: "invalid BytesPerToken",
			input: MaxModelLenInput{
				ModelTokenLimit:      4096,
				BytesPerToken:        0, // invalid
				TotalModelWeightSize: "7.5Gi",
				GPUMemoryBytes:       gib("24Gi"),
				GPUCount:             2,
				NumRequiredNodes:     1,
			},
			expected:    0,
			description: "should return 0 for invalid BytesPerToken",
		},
		{
			name: "model weights exceed available GPU memory",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        131072,
				TotalModelWeightSize: "40Gi", // exceeds usable memory on a 24Gi GPU
				GPUMemoryBytes:       gib("24Gi"),
				GPUCount:             1,
				NumRequiredNodes:     1,
			},
			expected:    0,
			description: "should return 0 when weights plus overhead leave no KV cache room",
		},
		{
			name: "deepseek-r1-distill-llama-8b on Standard_NV36ads_A10_v5",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        131072,
				TotalModelWeightSize: "14.96Gi",
				GPUMemoryBytes:       gib("24Gi"),
				GPUCount:             1,
				NumRequiredNodes:     1,
			},
			expected:    14848, // base 2.3Gi + 0.05*per-GPU weight overhead (small model => low overhead)
			description: "deepseek-r1-distill-llama-8b with vLLM on Standard_NV36ads_A10_v5",
		},
		{
			name: "deepseek-r1-distill-qwen-14b on Standard_NV72ads_A10_v5",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        196608,
				TotalModelWeightSize: "27.51Gi",
				GPUMemoryBytes:       gib("48Gi"),
				GPUCount:             2,
				NumRequiredNodes:     1,
			},
			expected:    34048, // base 2.3Gi + 0.05*per-GPU weight overhead
			description: "deepseek-r1-distill-qwen-14b with vLLM on Standard_NV72ads_A10_v5",
		},
		{
			name: "deepseek-r1-distill-qwen-14b on Standard_NC24ads_A100_v4",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        196608,
				TotalModelWeightSize: "27.51Gi",
				GPUMemoryBytes:       gib("80Gi"),
				GPUCount:             1,
				NumRequiredNodes:     1,
			},
			expected:    131072, // Clamped to ModelTokenLimit (original calculation: 192456)
			description: "deepseek-r1-distill-qwen-14b with vLLM on Standard_NC24ads_A100_v4",
		},
		{
			name: "llama-3.3-70b-instruct on Standard_NC24ads_A100_v4",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        327680,
				TotalModelWeightSize: "131.42Gi",
				GPUMemoryBytes:       gib("80Gi"),
				GPUCount:             1,
				NumRequiredNodes:     3,
			},
			// Multi-node: pipeline parallelism splits the 80 layers across the 3
			// nodes, so per-GPU KV is bytes/token / (gpuCount * nodes). With the
			// layers split, KV room is ample and the candidate clamps to the model
			// token limit.
			expected:    131072,
			description: "llama-3.3-70b-instruct with vLLM on 3 nodes x Standard_NC24ads_A100_v4",
		},
		{
			name: "mistral-small-4-119b MLA on Standard_NC80adis_H100_v5",
			input: MaxModelLenInput{
				ModelTokenLimit:      1048576,
				BytesPerToken:        23040, // MLA: 2 * (kvLoraRank 256 + qkRopeHeadDim 64) * 36 layers
				TotalModelWeightSize: "112.63Gi",
				MLAReplicatedKVCache: true,
				GPUMemoryBytes:       gib("188Gi"),
				GPUCount:             2,
				NumRequiredNodes:     1,
			},
			// MLA KV is replicated across the 2 TP ranks, so bytes/token is NOT
			// divided by GPUCount. Must stay under vLLM's ~810208 limit (would be
			// mis-computed as >1M and clamped to ModelTokenLimit without the MLA flag).
			expected:    761600,
			description: "mistral-small-4-119b (MLA) with vLLM on Standard_NC80adis_H100_v5",
		},
		{
			name: "minimax-m2.7 multi-node TP2xPP2 on Standard_NC80adis_H100_v5",
			input: MaxModelLenInput{
				ModelTokenLimit:      204800,
				BytesPerToken:        253952, // GQA: 2 * 8 kv-heads * 128 headDim * 62 layers
				TotalModelWeightSize: "214.34Gi",
				GPUMemoryBytes:       gib("188Gi"),
				GPUCount:             2,
				NumRequiredNodes:     2,
			},
			// TP=2 (shard KV heads) x PP=2 (split 62 layers across nodes) => per-GPU
			// bytes/token = 253952 / (2*2) = 63488, matching vLLM's measured ~63503.
			// KV room then exceeds the model token limit, so it clamps to 204800.
			// (Before the PP fix, KAITO computed 81408 and lost ~60% of context.)
			expected:    204800,
			description: "minimax-m2.7 (GQA) with vLLM on 2 nodes x Standard_NC80adis_H100_v5 (TP2xPP2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeMaxModelLen(tt.input)
			if result != tt.expected {
				t.Errorf("Test %s failed: expected %d, got %d", tt.name, tt.expected, result)
			}
			t.Logf("Test %s: %s - result: %d", tt.name, tt.description, result)
		})
	}
}
