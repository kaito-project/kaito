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
			name: "deepseek-r1-distill-llama-8b on Standard_NV36ads_A10_v5",
			input: MaxModelLenInput{
				ModelTokenLimit:      131072,
				BytesPerToken:        131072,
				TotalModelWeightSize: "14.96Gi",
				GPUMemoryBytes:       gib("24Gi"),
				GPUCount:             1,
				NumRequiredNodes:     1,
			},
			expected:    21248,
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
			expected:    41728,
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
			expected:    22016,
			description: "llama-3.3-70b-instruct with vLLM on 3 nodes x Standard_NC24ads_A100_v4",
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
