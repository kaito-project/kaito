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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A representative on-disk bundle size; the exact value only has to be positive
// and consistent, since it is now supplied rather than derived.
const testSizeBytes = int64(16060522496)

const llamaConfigJSON = `{
	"architectures": ["LlamaForCausalLM"],
	"hidden_size": 4096,
	"intermediate_size": 14336,
	"num_hidden_layers": 32,
	"num_attention_heads": 32,
	"num_key_value_heads": 8,
	"max_position_embeddings": 131072,
	"vocab_size": 128256,
	"torch_dtype": "bfloat16"
}`

func TestGenerateFromConfigDerivesServingParameters(t *testing.T) {
	param, err := GenerateFromConfig("custom-abc123", []byte(llamaConfigJSON), testSizeBytes)
	require.NoError(t, err)
	require.NotNil(t, param)

	assert.Equal(t, "custom-abc123", param.Metadata.Name)
	assert.Contains(t, param.Metadata.Architectures, "LlamaForCausalLM")
	assert.NotEmpty(t, param.Metadata.ModelFileSize, "a custom model must be sizable for node estimation")

	// Weights come from an operator-supplied bundle, so no HuggingFace
	// credentials are ever needed.
	assert.False(t, param.Metadata.DownloadAuthRequired)
}

func TestGenerateFromConfigMergesNestedTextConfig(t *testing.T) {
	// Multimodal checkpoints nest the language-model dimensions; sizing must
	// read them rather than fail on the outer object.
	config := `{
		"architectures": ["Qwen2VLForConditionalGeneration"],
		"text_config": {
			"hidden_size": 3584,
			"intermediate_size": 18944,
			"num_hidden_layers": 28,
			"num_attention_heads": 28,
			"num_key_value_heads": 4,
			"vocab_size": 152064
		},
		"torch_dtype": "bfloat16"
	}`

	param, err := GenerateFromConfig("custom-vl", []byte(config), testSizeBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, param.Metadata.ModelFileSize)
}

func TestGenerateFromConfigRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		config    string
		sizeBytes int64
		errSubstr string
	}{
		{
			name:      "empty model name",
			modelName: "",
			config:    llamaConfigJSON,
			sizeBytes: testSizeBytes,
			errSubstr: "model name is required",
		},
		{
			// Sizing is now entirely supplied, so a missing or nonsensical size
			// must fail rather than default to something plausible.
			name:      "zero size",
			modelName: "custom-x",
			config:    llamaConfigJSON,
			sizeBytes: 0,
			errSubstr: "positive number of bytes",
		},
		{
			name:      "negative size",
			modelName: "custom-x",
			config:    llamaConfigJSON,
			sizeBytes: -1,
			errSubstr: "positive number of bytes",
		},
		{
			name:      "malformed json",
			modelName: "custom-x",
			config:    `{"hidden_size": `,
			sizeBytes: testSizeBytes,
			errSubstr: "not valid JSON",
		},
		{
			name:      "empty object",
			modelName: "custom-x",
			config:    `{}`,
			sizeBytes: testSizeBytes,
			errSubstr: "empty",
		},
		{
			name:      "no architecture declared",
			modelName: "custom-x",
			config: `{
				"hidden_size": 4096,
				"intermediate_size": 14336,
				"num_hidden_layers": 32,
				"num_attention_heads": 32,
				"vocab_size": 32000,
				"torch_dtype": "bfloat16"
			}`,
			sizeBytes: testSizeBytes,
			errSubstr: "architecture",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateFromConfig(tt.modelName, []byte(tt.config), tt.sizeBytes)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestGenerateFromConfigAcceptsQuantizedCheckpoints(t *testing.T) {
	// The former FP8-only restriction existed because tensor accounting could not
	// size other formats. Sizing is now supplied, the runtime handles these
	// formats natively, and KV-cache sizing does not depend on weight dtype, so
	// there is nothing left for this path to get wrong.
	for _, method := range []string{"fp8", "awq", "gptq", "compressed-tensors"} {
		t.Run(method, func(t *testing.T) {
			config := fmt.Sprintf(`{
				"architectures": ["LlamaForCausalLM"],
				"hidden_size": 4096,
				"intermediate_size": 14336,
				"num_hidden_layers": 32,
				"num_attention_heads": 32,
				"num_key_value_heads": 8,
				"vocab_size": 128256,
				"torch_dtype": "bfloat16",
				"quantization_config": {"quant_method": %q}
			}`, method)

			param, err := GenerateFromConfig("custom-q", []byte(config), testSizeBytes)
			require.NoError(t, err)
			assert.Equal(t, method, param.Metadata.QuantMethod,
				"the declared method must be recorded; it selects the runtime's dtype handling")
		})
	}
}

func TestGenerateFromConfigRequiresNamedQuantizationMethod(t *testing.T) {
	// An unnamed quantization would be loaded as though the weights were dense.
	config := `{
		"architectures": ["LlamaForCausalLM"],
		"hidden_size": 4096,
		"intermediate_size": 14336,
		"num_hidden_layers": 32,
		"num_attention_heads": 32,
		"num_key_value_heads": 8,
		"vocab_size": 128256,
		"torch_dtype": "bfloat16",
		"quantization_config": {"bits": 4}
	}`

	_, err := GenerateFromConfig("custom-q", []byte(config), testSizeBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quantization method")
}

func TestGenerateFromConfigSizeIsUsedVerbatim(t *testing.T) {
	// The supplied size must reach the fields that drive capacity decisions,
	// rounded up so a partial gibibyte is never dropped.
	const oneGiB = int64(1) << 30

	exact, err := GenerateFromConfig("custom-a", []byte(llamaConfigJSON), 2*oneGiB)
	require.NoError(t, err)
	assert.Equal(t, "2Gi", exact.Metadata.ModelFileSize)

	partial, err := GenerateFromConfig("custom-b", []byte(llamaConfigJSON), 2*oneGiB+1)
	require.NoError(t, err)
	assert.Equal(t, "3Gi", partial.Metadata.ModelFileSize, "a partial gibibyte must round up")

	// DiskStorageRequirement is parsed from this string by trimming a "Gi"
	// suffix, so a non-conforming form would silently size the disk at zero.
	assert.Contains(t, exact.Metadata.DiskStorageRequirement, "Gi")
	assert.NotEqual(t, "0Gi", exact.Metadata.DiskStorageRequirement)
}
