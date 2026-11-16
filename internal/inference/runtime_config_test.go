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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
