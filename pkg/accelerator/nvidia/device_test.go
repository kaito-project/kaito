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

package nvidia

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/accelerator"
)

func TestDeviceFromNode(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		wantErr  bool
		expected *accelerator.Device
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
			expected: &accelerator.Device{
				Count:  2,
				Model:  "Tesla-V100-SXM2-32GB",
				Memory: resource.MustParse("64Gi"), // total node VRAM = 2 × 32Gi
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
			expected: &accelerator.Device{
				Count:             1,
				Model:             "A100-SXM4-80GB",
				Memory:            resource.MustParse("80Gi"),
				ComputeCapability: 8.0,
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
			expected: &accelerator.Device{
				Count:             1,
				Model:             "Tesla-T4",
				Memory:            resource.MustParse("16Gi"),
				ComputeCapability: 7.5,
			},
		},
		{
			name: "MIG active (mixed/single) sets Partitioned",
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
			expected: &accelerator.Device{
				Count:       3,
				Model:       "NVIDIA-H100-NVL",
				Memory:      resource.MustParse("72Gi"),
				Partitioned: true,
			},
		},
		{
			name: "MIG disabled does not set Partitioned",
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
			expected: &accelerator.Device{
				Count:       1,
				Model:       "NVIDIA-H100-NVL",
				Memory:      resource.MustParse("94Gi"),
				Partitioned: false,
			},
		},
		{
			name: "MIG config not yet applied (state != success) does not set Partitioned",
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
			expected: &accelerator.Device{
				Count:       3,
				Model:       "NVIDIA-H100-NVL",
				Memory:      resource.MustParse("72Gi"),
				Partitioned: false,
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
			got, err := Vendor{}.DeviceFromNode(tt.node)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.Count, got.Count)
				assert.Equal(t, tt.expected.Model, got.Model)
				assert.True(t, tt.expected.Memory.Cmp(got.Memory) == 0, "expected memory %s, got %s", tt.expected.Memory.String(), got.Memory.String())
				assert.Equal(t, tt.expected.ComputeCapability, got.ComputeCapability)
				assert.Equal(t, tt.expected.Partitioned, got.Partitioned)
			}
		})
	}
}
