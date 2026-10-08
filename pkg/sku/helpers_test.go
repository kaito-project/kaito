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
			name: "MIG active (mixed/single) sets IsMIG",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-mig",
					Labels: map[string]string{
						"nvidia.com/gpu.product":      "NVIDIA-H100-NVL",
						"nvidia.com/gpu.count":        "3",
						"nvidia.com/gpu.memory":       "24576",
						"nvidia.com/mig.config":       "all-2g.24gb",
						"nvidia.com/mig.config.state": "success",
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:      "unknown",
				GPUCount: 3,
				GPUModel: "NVIDIA-H100-NVL",
				GPUMem:   resource.MustParse("72Gi"),
				IsMIG:    true,
			},
		},
		{
			name: "MIG disabled does not set IsMIG",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-mig-disabled",
					Labels: map[string]string{
						"nvidia.com/gpu.product":      "NVIDIA-H100-NVL",
						"nvidia.com/gpu.count":        "1",
						"nvidia.com/gpu.memory":       "96256",
						"nvidia.com/mig.config":       "all-disabled",
						"nvidia.com/mig.config.state": "success",
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:      "unknown",
				GPUCount: 1,
				GPUModel: "NVIDIA-H100-NVL",
				GPUMem:   resource.MustParse("94Gi"),
				IsMIG:    false,
			},
		},
		{
			name: "MIG config not yet applied (state != success) does not set IsMIG",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-mig-pending",
					Labels: map[string]string{
						"nvidia.com/gpu.product":      "NVIDIA-H100-NVL",
						"nvidia.com/gpu.count":        "3",
						"nvidia.com/gpu.memory":       "24576",
						"nvidia.com/mig.config":       "all-2g.24gb",
						"nvidia.com/mig.config.state": "pending",
					},
				},
			},
			wantErr: false,
			expected: &GPUConfig{
				SKU:      "unknown",
				GPUCount: 3,
				GPUModel: "NVIDIA-H100-NVL",
				GPUMem:   resource.MustParse("72Gi"),
				IsMIG:    false,
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
			got, err := getGPUConfigFromNodeLabels(tt.node)
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
				assert.Equal(t, tt.expected.IsMIG, got.IsMIG)
			}
		})
	}
}

func TestGetGPUConfigFromNode(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	t.Run("known instance type does not require GFD labels", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "known-sku-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "Standard_NC24ads_A100_v4",
				},
			},
		}

		got, err := GetGPUConfigFromNode(node)
		assert.NoError(t, err)
		assert.Equal(t, "Standard_NC24ads_A100_v4", got.SKU)
		assert.Equal(t, 1, got.GPUCount)
		assert.Equal(t, "NVIDIA A100", got.GPUModel)
		assert.Equal(t, resource.MustParse("80Gi"), got.GPUMem)
	})

	t.Run("unknown instance type falls back to GFD labels", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "unknown-sku-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "custom-gpu-node",
					consts.NvidiaGPUProduct:        "Custom-GPU",
					consts.NvidiaGPUCount:          "2",
					consts.NvidiaGPUMemory:         "24576",
				},
			},
		}

		got, err := GetGPUConfigFromNode(node)
		assert.NoError(t, err)
		assert.Equal(t, UnknownSKU, got.SKU)
		assert.Equal(t, 2, got.GPUCount)
		assert.Equal(t, "Custom-GPU", got.GPUModel)
		assert.Equal(t, resource.MustParse("48Gi"), got.GPUMem)
	})

	t.Run("MIG reconfiguration uses GFD labels instead of the known instance type", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pending-mig-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "Standard_NC24ads_A100_v4",
					consts.NvidiaGPUProduct:        "NVIDIA-A100-SXM4-80GB",
					consts.NvidiaGPUCount:          "1",
					consts.NvidiaGPUMemory:         "81920",
					consts.NvidiaMIGConfig:         "all-1g.10gb",
					consts.NvidiaMIGConfigState:    "pending",
				},
			},
		}

		got, err := GetGPUConfigFromNode(node)
		assert.NoError(t, err)
		assert.Equal(t, UnknownSKU, got.SKU)
		assert.False(t, got.IsMIG)
	})

	t.Run("MIG node uses live GFD topology for a known instance type", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mig-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "Standard_NC24ads_A100_v4",
					consts.NvidiaGPUProduct:        "NVIDIA-A100-SXM4-80GB-MIG-1g.10gb",
					consts.NvidiaGPUCount:          "1",
					consts.NvidiaGPUMemory:         "10240",
					consts.NvidiaMIGConfig:         "all-1g.10gb",
					consts.NvidiaMIGConfigState:    "success",
				},
			},
		}

		got, err := GetGPUConfigFromNode(node)
		assert.NoError(t, err)
		assert.Equal(t, UnknownSKU, got.SKU)
		assert.Equal(t, 1, got.GPUCount)
		expectedMemory := resource.MustParse("10Gi")
		assert.Zero(t, expectedMemory.Cmp(got.GPUMem))
		assert.True(t, got.IsMIG)
	})
}

func TestScaleGPUConfigToCount(t *testing.T) {
	perGPU := int64(80) * consts.GiBToBytes
	nodeCfg := &GPUConfig{
		GPUCount: 8,
		GPUModel: "NVIDIA-A100",
		GPUMem:   *resource.NewQuantity(perGPU*8, resource.BinarySI),
	}

	t.Run("scales GPUCount and GPUMem to the requested count", func(t *testing.T) {
		got, err := ScaleGPUConfigToCount(nodeCfg, 2)
		assert.NoError(t, err)
		assert.Equal(t, 2, got.GPUCount)
		assert.Equal(t, perGPU*2, got.GPUMem.Value())
		assert.Equal(t, perGPU, got.GPUMem.Value()/int64(got.GPUCount))
		assert.Equal(t, 8, nodeCfg.GPUCount)
	})

	t.Run("count equal to node GPUs is allowed", func(t *testing.T) {
		got, err := ScaleGPUConfigToCount(nodeCfg, 8)
		assert.NoError(t, err)
		assert.Equal(t, 8, got.GPUCount)
	})

	t.Run("count exceeding node GPUs errors", func(t *testing.T) {
		_, err := ScaleGPUConfigToCount(nodeCfg, 9)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds GPUs available")
	})
}
