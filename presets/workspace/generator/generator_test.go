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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestGeneratePreset(t *testing.T) {
	// These expected values are derived from presets/workspace/generator/preset_generator_test.py
	cases := []struct {
		modelRepo     string
		expectedParam model.PresetParam
		expectedVLLM  model.VLLMParam
	}{
		{
			modelRepo: "microsoft/Phi-4-mini-instruct",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "phi-4-mini-instruct",
					Architectures:          []string{"Phi3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "microsoft/Phi-4-mini-instruct"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "8Gi",
					BytesPerToken:          131072,
					ModelTokenLimit:        131072,
					DiskStorageRequirement: "68Gi", // 8 + 60
				},
				AttnType: "GQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "phi-4-mini-instruct",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "tiiuae/falcon-7b-instruct",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "falcon-7b-instruct",
					Architectures:          []string{"FalconForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "tiiuae/falcon-7b-instruct"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "14Gi", // Python test expects 27Gi due to double counting (bin+safetensors). We fix this to use safetensors only.
					BytesPerToken:          8192,
					ModelTokenLimit:        2048,
					DiskStorageRequirement: "74Gi", // 14 + 60
				},
				AttnType: "MQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "falcon-7b-instruct",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "mistralai/Ministral-3-8B-Instruct-2512",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "ministral-3-8b-instruct-2512",
					Architectures:          []string{"Mistral3ForConditionalGeneration"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "mistralai/Ministral-3-8B-Instruct-2512"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "10Gi",
					BytesPerToken:          139264,
					ModelTokenLimit:        262144,
					DiskStorageRequirement: "70Gi", // 10 + 60
				},
				AttnType: "GQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "ministral-3-8b-instruct-2512",
				ModelRunParams: map[string]string{
					"load_format":    "mistral",
					"config_format":  "mistral",
					"tokenizer_mode": "mistral",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "mistralai/Mistral-Large-3-675B-Instruct-2512",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "mistral-large-3-675b-instruct-2512",
					Architectures:          []string{"MistralLarge3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "mistralai/Mistral-Large-3-675B-Instruct-2512"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "635Gi",
					BytesPerToken:          70272,
					ModelTokenLimit:        294912,
					DiskStorageRequirement: "695Gi", // 635 + 60
					ToolCallParser:         "mistral",
				},
				AttnType: "MLA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "mistral-large-3-675b-instruct-2512",
				ModelRunParams: map[string]string{
					"load_format":    "mistral",
					"config_format":  "mistral",
					"tokenizer_mode": "mistral",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "Qwen/Qwen3-Coder-30B-A3B-Instruct",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "qwen3-coder-30b-a3b-instruct",
					Architectures:          []string{"Qwen3MoeForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "Qwen/Qwen3-Coder-30B-A3B-Instruct"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "57Gi",
					BytesPerToken:          98304,
					ModelTokenLimit:        262144,
					DiskStorageRequirement: "117Gi", // 57 + 60
					ReasoningParser:        "qwen3",
					ToolCallParser:         "qwen3_xml",
				},
				AttnType: "GQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "qwen3-coder-30b-a3b-instruct",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "Qwen/Qwen3-8B",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "qwen3-8b",
					Architectures:          []string{"Qwen3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "Qwen/Qwen3-8B"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "16Gi",
					BytesPerToken:          147456,
					ModelTokenLimit:        40960,
					DiskStorageRequirement: "76Gi", // 16 + 60
					ReasoningParser:        "qwen3",
					ToolCallParser:         "hermes",
				},
				AttnType: "GQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "qwen3-8b",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "deepseek-ai/DeepSeek-V3.1",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "deepseek-v3.1",
					Architectures:          []string{"DeepseekV3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "deepseek-ai/DeepSeek-V3.1"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "642Gi",
					BytesPerToken:          70272,
					ModelTokenLimit:        163840,
					DiskStorageRequirement: "702Gi", // 642 + 60
					ReasoningParser:        "deepseek_v3",
					ToolCallParser:         "deepseek_v31",
				},
				AttnType: "MLA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "deepseek-v3.1",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "deepseek-ai/DeepSeek-V3",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "deepseek-v3",
					Architectures:          []string{"DeepseekV3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "deepseek-ai/DeepSeek-V3"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "642Gi",
					BytesPerToken:          70272,
					ModelTokenLimit:        163840,
					DiskStorageRequirement: "702Gi", // 642 + 60
					ReasoningParser:        "deepseek_v3",
					ToolCallParser:         "deepseek_v3",
				},
				AttnType: "MLA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "deepseek-v3",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
		{
			modelRepo: "nvidia/Nemotron-Orchestrator-8B",
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "nemotron-orchestrator-8b",
					Architectures:          []string{"Qwen3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "nvidia/Nemotron-Orchestrator-8B"),
					DownloadAtRuntime:      true,
					DownloadAuthRequired:   false,
					ModelFileSize:          "31Gi",
					BytesPerToken:          147456,
					ModelTokenLimit:        40960,
					DiskStorageRequirement: "81Gi", // 31 + 50
					ReasoningParser:        "qwen3",
					ToolCallParser:         "hermes",
				},
				AttnType: "GQA",
			},
			expectedVLLM: model.VLLMParam{
				ModelName: "nemotron-orchestrator-8b",
				ModelRunParams: map[string]string{
					"load_format":    "auto",
					"config_format":  "auto",
					"tokenizer_mode": "auto",
				},
				DisallowLoRA: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.modelRepo, func(t *testing.T) {
			param, err := GeneratePreset(tc.modelRepo, "")
			assert.NoError(t, err)
			assert.NotNil(t, param)

			// Metadata checks
			assert.Equal(t, tc.expectedParam.Name, param.Name)
			assert.Equal(t, tc.expectedParam.Architectures, param.Architectures)
			assert.Equal(t, tc.expectedParam.ModelType, param.ModelType)
			assert.Equal(t, tc.expectedParam.Version, param.Version)
			assert.Equal(t, tc.expectedParam.DownloadAtRuntime, param.DownloadAtRuntime)
			assert.Equal(t, tc.expectedParam.DownloadAuthRequired, param.DownloadAuthRequired)
			assert.Equal(t, tc.expectedParam.Metadata.ModelFileSize, param.Metadata.ModelFileSize)
			assert.Equal(t, tc.expectedParam.Metadata.BytesPerToken, param.Metadata.BytesPerToken)
			assert.Equal(t, tc.expectedParam.Metadata.ModelTokenLimit, param.Metadata.ModelTokenLimit)
			assert.Equal(t, tc.expectedParam.Metadata.ReasoningParser, param.Metadata.ReasoningParser)
			assert.Equal(t, tc.expectedParam.Metadata.ToolCallParser, param.Metadata.ToolCallParser)

			// Struct fields
			assert.Equal(t, tc.expectedParam.Metadata.DiskStorageRequirement, param.Metadata.DiskStorageRequirement)
			assert.Equal(t, tc.expectedParam.AttnType, param.AttnType)

			// VLLM checks
			assert.Equal(t, tc.expectedVLLM.ModelName, param.VLLM.ModelName)
			assert.Equal(t, tc.expectedVLLM.DisallowLoRA, param.VLLM.DisallowLoRA)
			assert.Equal(t, tc.expectedVLLM.ModelRunParams, param.VLLM.ModelRunParams)
		})
	}
}

// this test only makes sure that all keys in reasoningParserModeNamePrefixMap are lowercased
func TestReasoningParserMap(t *testing.T) {
	for key := range reasoningParserModeNamePrefixMap {
		assert.Equal(t, key, strings.ToLower(key), "reasoningParserModeNamePrefixMap key is not lowercased: %s", key)
	}
}

// this test only makes sure that all keys in toolCallParserModeNamePrefixMap are lowercased
func TestToolCallParserMap(t *testing.T) {
	for key := range toolCallParserModeNamePrefixMap {
		assert.Equal(t, key, strings.ToLower(key), "toolCallParserModeNamePrefixMap key is not lowercased: %s", key)
	}
}
