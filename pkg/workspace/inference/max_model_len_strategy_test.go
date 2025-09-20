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

package inference

import (
	"testing"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

func TestComputePlannedMaxModelLen(t *testing.T) {
	tests := []struct {
		name                string
		preset              *pkgmodel.PresetParam
		gpu                 *sku.GPUConfig
		nodeCountPerReplica int
		expected            int
		description         string
	}{
		{
			name:   "nil preset",
			preset: nil,
			gpu: &sku.GPUConfig{
				GPUMemGB: 24,
				GPUCount: 2,
			},
			nodeCountPerReplica: 1,
			expected:            0,
			description:         "should return 0 for nil preset",
		},
		{
			name: "invalid BytesPerToken",
			preset: &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name: "test-model",
				},
				ModelTokenLimit:         4096,
				BytesPerToken:           0, // Invalid
				TotalSafeTensorFileSize: "7.5Gi",
			},
			gpu: &sku.GPUConfig{
				GPUMemGB: 24,
				GPUCount: 2,
			},
			nodeCountPerReplica: 1,
			expected:            0,
			description:         "should return 0 for invalid BytesPerToken",
		},
		{
			name: "deepseek-r1-distill-llama-8b on Standard_NV36ads_A10_v5",
			preset: &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name: "deepseek-r1-distill-llama-8b",
				},
				ModelTokenLimit:         131072,    // max_position_embeddings from HF config
				BytesPerToken:           131072,    // From actual model config
				TotalSafeTensorFileSize: "14.96Gi", // From actual model config
			},
			gpu: &sku.GPUConfig{
				GPUMemGB: 24, // A10 has 24GB memory
				GPUCount: 1,  // Standard_NV36ads_A10_v5 has 1 GPU
			},
			nodeCountPerReplica: 1,
			expected:            17152, // Calculated result from actual formula
			description:         "deepseek-r1-distill-llama-8b with vLLM on Standard_NV36ads_A10_v5",
		},
		{
			name: "deepseek-r1-distill-qwen-14b on Standard_NV72ads_A10_v5",
			preset: &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name: "deepseek-r1-distill-qwen-14b",
				},
				ModelTokenLimit:         131072,    // max_position_embeddings from HF config
				BytesPerToken:           196608,    // From actual model config
				TotalSafeTensorFileSize: "27.51Gi", // From actual model config
			},
			gpu: &sku.GPUConfig{
				GPUMemGB: 48, // Standard_NV72ads_A10_v5 has 48GB memory
				GPUCount: 2,  // Standard_NV72ads_A10_v5 has 2 GPUs
			},
			nodeCountPerReplica: 1,
			expected:            36352, // Calculated result from actual formula
			description:         "deepseek-r1-distill-qwen-14b with vLLM on Standard_NV72ads_A10_v5",
		},
		{
			name: "deepseek-r1-distill-qwen-14b on Standard_NC24ads_A100_v4",
			preset: &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name: "deepseek-r1-distill-qwen-14b",
				},
				ModelTokenLimit:         131072,    // max_position_embeddings from HF config
				BytesPerToken:           196608,    // From actual model config
				TotalSafeTensorFileSize: "27.51Gi", // From actual model config
			},
			gpu: &sku.GPUConfig{
				GPUMemGB: 80, // A100 has 80GB memory
				GPUCount: 1,  // Standard_NC24ads_A100_v4 has 1 GPU
			},
			nodeCountPerReplica: 1,
			expected:            131072, // Clamped to ModelTokenLimit (original calculation: 192456)
			description:         "deepseek-r1-distill-qwen-14b with vLLM on Standard_NC24ads_A100_v4",
		},
		{
			name: "llama-3.3-70b-instruct on Standard_NC24ads_A100_v4",
			preset: &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name: "llama-3.3-70b-instruct",
				},
				ModelTokenLimit:         131072,     // max_position_embeddings from HF config
				BytesPerToken:           327680,     // From actual model config
				TotalSafeTensorFileSize: "131.42Gi", // From actual model config
			},
			gpu: &sku.GPUConfig{
				GPUMemGB: 80, // A100 has 80GB memory
				GPUCount: 1,  // Standard_NC24ads_A100_v4 has 1 GPU per node
			},
			nodeCountPerReplica: 3,
			expected:            131072, // Clamped to ModelTokenLimit (original calculation: 183014)
			description:         "llama-3.3-70b-instruct with vLLM on 3 nodes x Standard_NC24ads_A100_v4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computePlannedMaxModelLen(tt.preset, tt.gpu, tt.nodeCountPerReplica)
			if result != tt.expected {
				t.Errorf("Test %s failed: expected %d, got %d", tt.name, tt.expected, result)
			}
			t.Logf("Test %s: %s - result: %d", tt.name, tt.description, result)
		})
	}
}
