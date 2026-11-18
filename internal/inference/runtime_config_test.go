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
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

func TestParseInferenceRuntimeConfig(t *testing.T) {
	config, err := parseInferenceRuntimeConfig("kv_cache_cpu_memory_utilization: 0\nvllm:\n  kv-transfer-config: '{\"kv_connector\":\"Custom\"}'\n")
	require.NoError(t, err)
	require.NotNil(t, config.KVCacheCPUMemoryUtilization)
	assert.Equal(t, 0.0, *config.KVCacheCPUMemoryUtilization)
	_, found := config.VLLM["kv-transfer-config"]
	assert.True(t, found)
	_, found = config.VLLM["max-model-len"]
	assert.False(t, found)

	config, err = parseInferenceRuntimeConfig("vllm:\n  max-model-len: 4096\n")
	require.NoError(t, err)
	assert.Nil(t, config.KVCacheCPUMemoryUtilization)

	_, err = parseInferenceRuntimeConfig("vllm: [")
	assert.Error(t, err)
}

func TestAdaptiveVerificationSupported(t *testing.T) {
	for _, tc := range []struct {
		name             string
		runParams        map[string]string
		userConfig       map[string]any
		pipelineParallel bool
		wantSupported    bool
		wantMode         string
		wantError        string
	}{
		{name: "defaults", wantSupported: true},
		{name: "node-derived pipeline parallelism", pipelineParallel: true},
		{name: "preset pipeline parallelism", runParams: map[string]string{"pipeline-parallel-size": "2"}},
		{name: "user pipeline parallelism", userConfig: map[string]any{"pipeline-parallel-size": 2}},
		{name: "quoted pipeline size", userConfig: map[string]any{"pipeline-parallel-size": "2"}},
		{name: "user pipeline size wins", pipelineParallel: true, userConfig: map[string]any{"pipeline-parallel-size": 1}, wantSupported: true},
		{name: "eager preset", runParams: map[string]string{"enforce-eager": ""}},
		{name: "eager user override", userConfig: map[string]any{"enforce-eager": true}},
		{name: "user disables eager preset", runParams: map[string]string{"enforce-eager": ""}, userConfig: map[string]any{"enforce-eager": false}, wantSupported: true},
		{name: "preset disables graphs", runParams: map[string]string{"compilation-config.cudagraph_mode": "NONE"}},
		{name: "piecewise only", userConfig: map[string]any{"compilation-config.cudagraph_mode": "PIECEWISE"}, wantMode: "PIECEWISE"},
		{name: "numeric no graphs", userConfig: map[string]any{"compilation-config.cudagraph_mode": 0}, wantMode: "NONE"},
		{name: "numeric piecewise", userConfig: map[string]any{"compilation-config.cudagraph_mode": 1}, wantMode: "PIECEWISE"},
		{name: "numeric full graphs", userConfig: map[string]any{"compilation-config.cudagraph_mode": 2}, wantMode: "FULL", wantSupported: true},
		{name: "JSON graph mode", userConfig: map[string]any{"compilation-config": `{"cudagraph_mode":"NONE"}`}, wantMode: "NONE"},
		{name: "JSON without graph override", userConfig: map[string]any{"compilation-config": `{"mode":3}`}, wantSupported: true},
		{
			name: "dotted graph mode wins",
			userConfig: map[string]any{
				"compilation-config":                `{"cudagraph_mode":"NONE"}`,
				"compilation-config.cudagraph_mode": "FULL",
			},
			wantMode: "FULL", wantSupported: true,
		},
		{name: "invalid pipeline size", userConfig: map[string]any{"pipeline-parallel-size": 0}, wantError: "pipeline-parallel-size"},
		{name: "fractional pipeline size", userConfig: map[string]any{"pipeline-parallel-size": 1.5}, wantError: "pipeline-parallel-size"},
		{name: "invalid eager value", userConfig: map[string]any{"enforce-eager": "maybe"}, wantError: "enforce-eager"},
		{name: "invalid graph mode", userConfig: map[string]any{"compilation-config.cudagraph_mode": "invalid"}, wantError: "cudagraph_mode"},
		{name: "empty graph mode", userConfig: map[string]any{"compilation-config.cudagraph_mode": ""}, wantError: "cudagraph_mode"},
		{name: "invalid JSON", userConfig: map[string]any{"compilation-config": "{"}, wantError: "compilation-config"},
		{name: "non-object JSON", userConfig: map[string]any{"compilation-config": "null"}, wantError: "JSON object"},
		{name: "non-string JSON", userConfig: map[string]any{"compilation-config": map[string]any{"cudagraph_mode": "NONE"}}, wantError: "JSON string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := &pkgmodel.PresetParam{RuntimeParam: pkgmodel.RuntimeParam{
				VLLM: pkgmodel.VLLMParam{ModelRunParams: tc.runParams},
			}}
			before := params.DeepCopy()
			beforeConfig := maps.Clone(tc.userConfig)
			supported, mode, err := adaptiveVerificationSupported(params, tc.userConfig, tc.pipelineParallel, nil)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantSupported, supported)
				assert.Equal(t, tc.wantMode, mode)
			}
			assert.Equal(t, before, params)
			assert.Equal(t, beforeConfig, tc.userConfig)
		})
	}

	t.Run("GPU graph policy is resolved before injection", func(t *testing.T) {
		params := &pkgmodel.PresetParam{RuntimeParam: pkgmodel.RuntimeParam{
			VLLM: pkgmodel.VLLMParam{ModelName: "deepseek-v4-flash-0731"},
		}}
		supported, _, err := adaptiveVerificationSupported(params, nil, false, &sku.GPUConfig{GPUModel: "NVIDIA H100"})
		require.NoError(t, err)
		assert.False(t, supported)
	})
}
