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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func bytesOf(q string) int64 {
	v := resource.MustParse(q)
	return v.Value()
}

func params(total, offloaded string) *pkgmodel.PresetParam {
	return &pkgmodel.PresetParam{
		Metadata:                pkgmodel.Metadata{CPUOffloadedWeightSize: offloaded},
		TotalSafeTensorFileSize: total,
	}
}

func TestResidentWeightBytes(t *testing.T) {
	got, err := ResidentWeightBytes(params("475.26Gi", "183.1Gi"))
	require.NoError(t, err)
	assert.Equal(t, bytesOf("475.26Gi")-bytesOf("183.1Gi"), got)

	got, err = ResidentWeightBytes(params("155.43Gi", ""))
	require.NoError(t, err)
	assert.Equal(t, bytesOf("155.43Gi"), got)

	got, err = ResidentWeightBytes(params("10Gi", "20Gi"))
	require.NoError(t, err)
	assert.Zero(t, got, "offloaded size larger than the checkpoint clamps to zero")

	_, err = ResidentWeightBytes(params("", ""))
	assert.Error(t, err)
	_, err = ResidentWeightBytes(params("10Gi", "lots"))
	assert.Error(t, err)
}

func TestKVHeadroomBytesPerGPU(t *testing.T) {
	nc80adis := &sku.GPUConfig{GPUCount: 2, GPUMem: resource.MustParse("188Gi"), GPUModel: "NVIDIA H100"}
	nd96isr := &sku.GPUConfig{GPUCount: 8, GPUMem: resource.MustParse("640Gi"), GPUModel: "NVIDIA H100"}
	gib := float64(consts.GiBToBytes)

	tests := []struct {
		name      string
		params    *pkgmodel.PresetParam
		gpuConfig *sku.GPUConfig
		numNodes  int
		wantGiB   float64
	}{
		// Measured with DSpark: 3.0 GiB of KV cache per GPU, 7.8K-token context.
		{"DeepSeek-V4-Flash on NC80adis", params("155.43Gi", ""), nc80adis, 1, 0.95},
		{"DeepSeek-V4-Flash on 8x H100", params("155.43Gi", ""), nd96isr, 1, 50.49},
		// Measured with DSpark: 2.66 GiB per GPU, 1.39M-token context.
		{"DeepSeek-V4.1-Flash on 8x H100", params("475.26Gi", "183.1Gi"), nd96isr, 1, 32.19},
		{"DeepSeek-V4.1-Flash on 8x H100 counting offloaded weights", params("475.26Gi", ""), nd96isr, 1, 7.67},
		{"weights shard across nodes", params("155.43Gi", ""), nc80adis, 2, 42.56},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := KVHeadroomBytesPerGPU(tc.params, tc.gpuConfig, tc.numNodes)
			require.True(t, ok)
			assert.InDelta(t, tc.wantGiB, got/gib, 0.01)
		})
	}

	for name, tc := range map[string]struct {
		params    *pkgmodel.PresetParam
		gpuConfig *sku.GPUConfig
	}{
		"nil params":         {nil, nc80adis},
		"nil gpu config":     {params("10Gi", ""), nil},
		"zero gpu memory":    {params("10Gi", ""), &sku.GPUConfig{GPUCount: 1}},
		"unknown size":       {params("", ""), nc80adis},
		"all weights in CPU": {params("10Gi", "10Gi"), nc80adis},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := KVHeadroomBytesPerGPU(tc.params, tc.gpuConfig, 1)
			assert.False(t, ok)
		})
	}
}
