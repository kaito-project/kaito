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

	"github.com/kaito-project/kaito/pkg/accelerator"
	"github.com/kaito-project/kaito/pkg/accelerator/nvidia"
	"github.com/kaito-project/kaito/pkg/consts"
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
		assert.Equal(t, accelerator.TypeNVIDIA, got.Accelerator)
		assert.Equal(t, nvidia.GPUModelA100, got.CanonicalGPUModel())
	})

	t.Run("unknown instance type falls back to GFD labels", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "unknown-sku-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "custom-gpu-node",
					nvidia.LabelGPUProduct:         "Custom-GPU",
					nvidia.LabelGPUCount:           "2",
					nvidia.LabelGPUMemory:          "24576",
				},
			},
		}

		got, err := GetGPUConfigFromNode(node)
		assert.NoError(t, err)
		assert.Equal(t, UnknownSKU, got.SKU)
		assert.Equal(t, 2, got.GPUCount)
		assert.Equal(t, "Custom-GPU", got.GPUModel)
		assert.Equal(t, resource.MustParse("48Gi"), got.GPUMem)
		assert.Equal(t, accelerator.TypeNVIDIA, got.Accelerator)
	})

	t.Run("MIG reconfiguration uses GFD labels instead of the known instance type", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pending-mig-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "Standard_NC24ads_A100_v4",
					nvidia.LabelGPUProduct:         "NVIDIA-A100-SXM4-80GB",
					nvidia.LabelGPUCount:           "1",
					nvidia.LabelGPUMemory:          "81920",
					nvidia.LabelMIGConfig:          "all-1g.10gb",
					nvidia.LabelMIGConfigState:     "pending",
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
					nvidia.LabelGPUProduct:         "NVIDIA-A100-SXM4-80GB-MIG-1g.10gb",
					nvidia.LabelGPUCount:           "1",
					nvidia.LabelGPUMemory:          "10240",
					nvidia.LabelMIGConfig:          "all-1g.10gb",
					nvidia.LabelMIGConfigState:     "success",
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
		assert.Equal(t, nvidia.GPUModelA100, got.CanonicalGPUModel())
	})
}

func TestScaleGPUConfigToCount(t *testing.T) {
	perGPU := int64(80) * GiB
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
