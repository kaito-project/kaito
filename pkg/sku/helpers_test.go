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

package sku

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestGetSKUHandler(t *testing.T) {
	t.Run("azure provider returns handler", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
		h, err := GetSKUHandler()
		assert.NoError(t, err)
		assert.NotNil(t, h)
	})

	t.Run("unknown provider returns error", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", "unknown-cloud")
		_, err := GetSKUHandler()
		assert.Error(t, err)
	})

	t.Run("empty provider returns error", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", "")
		_, err := GetSKUHandler()
		assert.Error(t, err)
	})
}

func TestGetGPUConfigFromNvidiaLabels(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		wantErr  bool
		expected *GPUConfig
	}{
		{
			name: "valid nvidia.com labels",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.product": "Tesla-V100-SXM2-32GB",
						"nvidia.com/gpu.count":   "2",
						"nvidia.com/gpu.memory":  "32768", // 32GiB per GPU in MiB
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:      "unknown",
				GPUCount: 2,
				GPUModel: "Tesla-V100-SXM2-32GB",
				GPUMem:   resource.MustParse("64Gi"), // total node VRAM = 2 × 32Gi
			},
		},
		{
			name: "valid labels with CUDA compute capability",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-a100",
					Labels: map[string]string{
						"nvidia.com/gpu.product":        "A100-SXM4-80GB",
						"nvidia.com/gpu.count":          "1",
						"nvidia.com/gpu.memory":         "81920",
						"nvidia.com/cuda.compute.major": "8",
						"nvidia.com/cuda.compute.minor": "0",
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:                   "unknown",
				GPUCount:              1,
				GPUModel:              "A100-SXM4-80GB",
				GPUMem:                resource.MustParse("80Gi"),
				CUDAComputeCapability: 8.0,
			},
		},
		{
			name: "valid labels with CUDA compute capability 7.5",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-t4",
					Labels: map[string]string{
						"nvidia.com/gpu.product":        "Tesla-T4",
						"nvidia.com/gpu.count":          "1",
						"nvidia.com/gpu.memory":         "16384",
						"nvidia.com/cuda.compute.major": "7",
						"nvidia.com/cuda.compute.minor": "5",
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:                   "unknown",
				GPUCount:              1,
				GPUModel:              "Tesla-T4",
				GPUMem:                resource.MustParse("16Gi"),
				CUDAComputeCapability: 7.5,
			},
		},
		{
			name: "missing nvidia.com/gpu.product label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.count":  "1",
						"nvidia.com/gpu.memory": "16384",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing nvidia.com/gpu.count label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.product": "Tesla-T4",
						"nvidia.com/gpu.memory":  "16384",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing nvidia.com/gpu.memory label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.product": "Tesla-T4",
						"nvidia.com/gpu.count":   "1",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid nvidia.com/gpu.count value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.product": "Tesla-T4",
						"nvidia.com/gpu.count":   "invalid",
						"nvidia.com/gpu.memory":  "16384",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid nvidia.com/gpu.memory value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node",
					Labels: map[string]string{
						"nvidia.com/gpu.product": "Tesla-T4",
						"nvidia.com/gpu.count":   "1",
						"nvidia.com/gpu.memory":  "invalid",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGPUConfigFromNodeLabels(tt.node)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.SKU, got.SKU)
				assert.Equal(t, tt.expected.GPUCount, got.GPUCount)
				assert.Equal(t, tt.expected.GPUModel, got.GPUModel)
				assert.True(t, tt.expected.GPUMem.Cmp(got.GPUMem) == 0, "expected GPUMem %s, got %s", tt.expected.GPUMem.String(), got.GPUMem.String())
				assert.Equal(t, tt.expected.CUDAComputeCapability, got.CUDAComputeCapability)
			}
		})
	}
}
