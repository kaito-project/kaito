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

package maxnumseqestimator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

func TestMaxNumSeqsEstimator_Estimate(t *testing.T) {
	h100 := &sku.GPUConfig{
		SKU:      "Standard_NC40ads_H100_v5",
		GPUCount: 1,
		GPUMem:   resource.MustParse("94Gi"),
		GPUModel: "NVIDIA H100",
	}
	a100 := &sku.GPUConfig{
		SKU:      "Standard_NC24ads_A100_v4",
		GPUCount: 1,
		GPUMem:   resource.MustParse("80Gi"),
		GPUModel: "NVIDIA A100",
	}

	cases := []struct {
		name          string
		modelName     string
		perLayerBytes int
		numFull       int
		numLinear     int
		weights       string
		gpu           *sku.GPUConfig
		numNodes      int
		wantOK        bool
		wantMin       int // inclusive lower bound on the returned cap
		wantMax       int // inclusive upper bound on the returned cap
		// wantBelowBlocks, when > 0, asserts the returned cap is strictly below the
		// real vLLM Mamba-cache-block count observed for that model/GPU.
		wantBelowBlocks int
	}{
		{
			// Qwen3.6-27B / Qwen3.8-27B: 48 linear + 16 full-attn layers,
			// 3207168 B/linear layer. vLLM measured 614 available blocks on
			// this GPU; stay below.
			name:            "qwen3.6-27b on single h100",
			modelName:       "qwen3.6-27b",
			perLayerBytes:   3207168,
			numFull:         16,
			numLinear:       48,
			weights:         "51.75Gi",
			gpu:             h100,
			numNodes:        1,
			wantOK:          true,
			wantMin:         500,
			wantMax:         600,
			wantBelowBlocks: 614,
		},
		{
			name:            "qwen3.8-27b on single h100",
			modelName:       "qwen3.8-27b",
			perLayerBytes:   3207168,
			numFull:         16,
			numLinear:       48,
			weights:         "51.75Gi",
			gpu:             h100,
			numNodes:        1,
			wantOK:          true,
			wantMin:         500,
			wantMax:         600,
			wantBelowBlocks: 614,
		},
		{
			name:            "qwen3.6-35b-a3b on single h100",
			modelName:       "qwen3.6-35b-a3b",
			perLayerBytes:   2146304,
			numFull:         10,
			numLinear:       30,
			weights:         "66.97Gi",
			gpu:             h100,
			numNodes:        1,
			wantOK:          true,
			wantMin:         500,
			wantMax:         620,
			wantBelowBlocks: 747,
		},
		{
			// Measured on Standard_NC24ads_A100_v4: 391 available blocks. vLLM
			// name-excludes A100 from its large-GPU branch, so its own default is
			// already 256 and no cap is needed.
			name:          "qwen3.8-27b on single a100 keeps vllm default",
			modelName:     "qwen3.8-27b",
			perLayerBytes: 3207168,
			numFull:       16,
			numLinear:     48,
			weights:       "51.75Gi",
			gpu:           a100,
			numNodes:      1,
			wantOK:        false,
		},
		{
			// Tiny hybrid model leaves room for far more than the vLLM default, so
			// the estimator must not lower it.
			name:          "small hybrid keeps vllm default",
			modelName:     "tiny-hybrid",
			perLayerBytes: 100000,
			numFull:       4,
			numLinear:     12,
			weights:       "4Gi",
			gpu:           h100,
			numNodes:      1,
			wantOK:        false,
		},
		{
			name:          "pure attention model is not capped",
			modelName:     "qwen3.6-27b",
			perLayerBytes: 0,
			numFull:       0,
			numLinear:     0,
			weights:       "16Gi",
			gpu:           h100,
			numNodes:      1,
			wantOK:        false,
		},
		{
			name:          "multi-node is not capped",
			modelName:     "qwen3.6-27b",
			perLayerBytes: 3207168,
			numFull:       16,
			numLinear:     48,
			weights:       "51.75Gi",
			gpu:           h100,
			numNodes:      2,
			wantOK:        false,
		},
	}

	c := &MaxNumSeqsEstimator{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := &pkgmodel.PresetParam{
				Metadata: pkgmodel.Metadata{
					Name:                    tc.modelName,
					MambaStateBytesPerLayer: tc.perLayerBytes,
					NumFullAttnLayers:       tc.numFull,
					NumLinearLayers:         tc.numLinear,
				},
				TotalSafeTensorFileSize: tc.weights,
			}
			got, ok := c.Estimate(MaxNumSeqsEstimateRequest{
				WorkspaceName:   "test",
				InferenceParams: params,
				GPUConfig:       tc.gpu,
				NumNodes:        tc.numNodes,
			})
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				assert.Equal(t, 0, got)
				return
			}
			assert.GreaterOrEqual(t, got, tc.wantMin)
			assert.LessOrEqual(t, got, tc.wantMax)
			assert.Less(t, got, vLLMDefaultMaxNumSeqs(tc.gpu), "cap must be below the vLLM default")
			if tc.wantBelowBlocks > 0 {
				assert.Less(t, got, tc.wantBelowBlocks, "cap must be below the real available Mamba blocks")
			}
		})
	}
}

func TestMaxNumSeqsEstimator_Estimate_NilInputs(t *testing.T) {
	c := &MaxNumSeqsEstimator{}

	got, ok := c.Estimate(MaxNumSeqsEstimateRequest{NumNodes: 1})
	assert.False(t, ok)
	assert.Equal(t, 0, got)

	// Hybrid model but no GPU config resolved.
	got, ok = c.Estimate(MaxNumSeqsEstimateRequest{
		InferenceParams: &pkgmodel.PresetParam{
			Metadata:                pkgmodel.Metadata{Name: "qwen3.6-27b", MambaStateBytesPerLayer: 3207168, NumFullAttnLayers: 16, NumLinearLayers: 48},
			TotalSafeTensorFileSize: "51.75Gi",
		},
		NumNodes: 1,
	})
	assert.False(t, ok)
	assert.Equal(t, 0, got)
}

func TestResolveGroupSize(t *testing.T) {
	cases := []struct {
		name      string
		numLinear int
		numFull   int
		want      int
	}{
		// Real catalog models: full attention is the minority type and the counts
		// are far enough apart that vLLM keeps the minimum.
		{"qwen 27b 48:16", 48, 16, 16},
		{"qwen 35b-a3b 30:10", 30, 10, 10},
		{"nemotron nano 9b 27:4", 27, 4, 4},
		// Within 1.5x, so vLLM pads up to the larger count instead.
		{"close counts pad up to max", 20, 16, 20},
		{"equal counts", 16, 16, 16},
		// Exactly 1.5x is not < 1.5x, so the minimum wins.
		{"exactly 1.5x keeps min", 24, 16, 16},
		// Linear layers as the minority type must still pick the minimum.
		{"linear is minority", 8, 40, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveGroupSize(tc.numLinear, tc.numFull))
		})
	}
}

func TestVLLMDefaultMaxNumSeqs(t *testing.T) {
	cases := []struct {
		name     string
		gpuCount int
		gpuMem   string
		gpuModel string
		want     int
	}{
		{"h100 94Gi", 1, "94Gi", "NVIDIA H100", 1024},
		{"h100 per-gpu memory is what counts", 2, "188Gi", "NVIDIA H100", 1024},
		{"a100 80Gi is name-excluded upstream", 1, "80Gi", "NVIDIA A100", 256},
		{"a10 24Gi is below the threshold", 1, "24Gi", "NVIDIA A10", 256},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vLLMDefaultMaxNumSeqs(&sku.GPUConfig{
				GPUCount: tc.gpuCount,
				GPUMem:   resource.MustParse(tc.gpuMem),
				GPUModel: tc.gpuModel,
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMaxNumSeqsEstimator_Name(t *testing.T) {
	assert.Equal(t, "max-num-seqs-estimator", (&MaxNumSeqsEstimator{}).Name())
}
