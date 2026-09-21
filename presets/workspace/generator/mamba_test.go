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

package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// qwenLayerTypes builds a layer_types list with the given number of
// linear-attention and full-attention layers (order is irrelevant to counting).
func qwenLayerTypes(numLinear, numFull int) []interface{} {
	lt := make([]interface{}, 0, numLinear+numFull)
	for i := 0; i < numLinear; i++ {
		lt = append(lt, "linear_attention")
	}
	for i := 0; i < numFull; i++ {
		lt = append(lt, "full_attention")
	}
	return lt
}

func TestComputeMambaLayerInfo(t *testing.T) {
	cases := []struct {
		name         string
		config       map[string]interface{}
		wantPerLayer int
		wantLinear   int
		wantFull     int
	}{
		{
			// Gated DeltaNet dims of Qwen3.6-27B / Qwen3.8-27B.
			// conv = (128*16*2 + 128*48)*(4-1)*2 = 61440;
			// temporal = 48*128*128*4 = 3145728; per-layer = 3207168.
			name: "gated delta net qwen 27b",
			config: map[string]interface{}{
				"linear_key_head_dim":    float64(128),
				"linear_value_head_dim":  float64(128),
				"linear_num_key_heads":   float64(16),
				"linear_num_value_heads": float64(48),
				"linear_conv_kernel_dim": float64(4),
				"mamba_ssm_dtype":        "float32",
				"layer_types":            qwenLayerTypes(48, 16),
			},
			wantPerLayer: 3207168,
			wantLinear:   48,
			wantFull:     16,
		},
		{
			// Qwen3.6-35B-A3B: nv=32, 30 linear + 10 full.
			// conv = (128*16*2 + 128*32)*3*2 = 49152; temporal = 32*128*128*4 = 2097152.
			name: "gated delta net qwen 35b-a3b",
			config: map[string]interface{}{
				"linear_key_head_dim":    float64(128),
				"linear_value_head_dim":  float64(128),
				"linear_num_key_heads":   float64(16),
				"linear_num_value_heads": float64(32),
				"linear_conv_kernel_dim": float64(4),
				"mamba_ssm_dtype":        "float32",
				"layer_types":            qwenLayerTypes(30, 10),
			},
			wantPerLayer: 2146304,
			wantLinear:   30,
			wantFull:     10,
		},
		{
			// Mamba-2 (NemotronH-style) via hybrid_override_pattern.
			// convDim = 128*64 + 2*8*128 = 10240; conv = 10240*3*2 = 61440;
			// ssm = 128*64*128*4 = 4194304; per-layer = 4255744.
			name: "mamba2 hybrid_override_pattern",
			config: map[string]interface{}{
				"ssm_state_size":          float64(128),
				"conv_kernel":             float64(4),
				"mamba_num_heads":         float64(128),
				"mamba_head_dim":          float64(64),
				"n_groups":                float64(8),
				"hybrid_override_pattern": "M*M-M*",
			},
			wantPerLayer: 4255744,
			wantLinear:   3,
			wantFull:     2,
		},
		{
			name: "pure attention returns zero",
			config: map[string]interface{}{
				"num_key_value_heads": float64(8),
				"num_hidden_layers":   float64(32),
			},
			wantPerLayer: 0,
			wantLinear:   0,
			wantFull:     0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := computeMambaLayerInfo(tc.config)
			assert.Equal(t, tc.wantPerLayer, info.PerLayerBytes, "per-layer bytes")
			assert.Equal(t, tc.wantLinear, info.NumLinearLayers, "linear layers")
			assert.Equal(t, tc.wantFull, info.NumFullAttnLayers, "full attention layers")

			// computeMambaStateBytesPerSeq is the total across linear layers.
			assert.Equal(t, tc.wantPerLayer*tc.wantLinear, computeMambaStateBytesPerSeq(tc.config))
		})
	}
}
