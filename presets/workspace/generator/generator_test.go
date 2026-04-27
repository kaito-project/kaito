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
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"

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
					DiskStorageRequirement: "88Gi", // 8 + 80
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
					DiskStorageRequirement: "94Gi", // 14 + 80
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
					DiskStorageRequirement: "90Gi", // 10 + 80
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
					DiskStorageRequirement: "715Gi", // 635 + 80
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
					DiskStorageRequirement: "137Gi", // 57 + 80
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
					DiskStorageRequirement: "96Gi", // 16 + 80
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
					DiskStorageRequirement: "722Gi", // 642 + 80
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
					DiskStorageRequirement: "722Gi", // 642 + 80
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
					DiskStorageRequirement: "111Gi", // 31 + 80
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

func TestTextConfigMerging(t *testing.T) {
	cases := []struct {
		name           string
		modelConfig    map[string]interface{}
		expectedHidden int
		expectedLayers int
		expectedHeads  int
		expectedKV     int
		expectedToken  int
	}{
		{
			name: "text_config fields merged into top-level",
			modelConfig: map[string]interface{}{
				"architectures":           []interface{}{"Gemma3ForConditionalGeneration"},
				"max_position_embeddings": float64(8192),
				"text_config": map[string]interface{}{
					"hidden_size":         float64(2560),
					"num_hidden_layers":   float64(34),
					"num_attention_heads": float64(8),
					"num_key_value_heads": float64(4),
				},
			},
			expectedHidden: 2560,
			expectedLayers: 34,
			expectedHeads:  8,
			expectedKV:     4,
			expectedToken:  8192,
		},
		{
			name: "top-level values take precedence over text_config",
			modelConfig: map[string]interface{}{
				"architectures":           []interface{}{"TestModel"},
				"max_position_embeddings": float64(4096),
				"hidden_size":             float64(1024),
				"text_config": map[string]interface{}{
					"hidden_size":       float64(2048),
					"num_hidden_layers": float64(12),
				},
			},
			expectedHidden: 1024, // top-level wins
			expectedLayers: 12,   // only in text_config
			expectedHeads:  0,    // absent everywhere
			expectedKV:     0,    // absent everywhere
			expectedToken:  4096,
		},
		{
			name: "no text_config present",
			modelConfig: map[string]interface{}{
				"architectures":           []interface{}{"PlainModel"},
				"max_position_embeddings": float64(2048),
				"hidden_size":             float64(768),
				"num_hidden_layers":       float64(12),
				"num_attention_heads":     float64(12),
				"num_key_value_heads":     float64(4),
			},
			expectedHidden: 768,
			expectedLayers: 12,
			expectedHeads:  12,
			expectedKV:     4,
			expectedToken:  2048,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Apply the same text_config merge logic as FetchModelMetadata
			config := tc.modelConfig
			if textConfig, ok := config["text_config"].(map[string]interface{}); ok {
				for k, v := range textConfig {
					if _, exists := config[k]; !exists {
						config[k] = v
					}
				}
			}

			assert.Equal(t, tc.expectedHidden, getInt(config, configKeyMap["hiddenSize"], 0))
			assert.Equal(t, tc.expectedLayers, getInt(config, configKeyMap["numHiddenLayers"], 0))
			assert.Equal(t, tc.expectedHeads, getInt(config, configKeyMap["numAttentionHeads"], 0))
			assert.Equal(t, tc.expectedKV, getInt(config, configKeyMap["numKeyValueHeads"], 0))
			assert.Equal(t, tc.expectedToken, getInt(config, configKeyMap["modelTokenLimit"], 0))
		})
	}
}

func TestDimConfigKey(t *testing.T) {
	// The "dim" key was added to configKeyMap["hiddenSize"] for Mistral models
	// whose params.json uses "dim" instead of "hidden_size".
	config := map[string]interface{}{
		"dim":      float64(4096),
		"n_layers": float64(32),
	}
	assert.Equal(t, 4096, getInt(config, configKeyMap["hiddenSize"], 0))

	// "hidden_size" should still take priority over "dim"
	configWithBoth := map[string]interface{}{
		"hidden_size": float64(2048),
		"dim":         float64(4096),
	}
	assert.Equal(t, 2048, getInt(configWithBoth, configKeyMap["hiddenSize"], 0))
}

func TestLoadFromCatalog(t *testing.T) {
	cases := []struct {
		modelRepo     string
		expectFound   bool
		expectedParam model.PresetParam
	}{
		{
			modelRepo:   "microsoft/Phi-4-mini-instruct",
			expectFound: true,
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "phi-4-mini-instruct",
					Architectures:          []string{"Phi3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "microsoft/Phi-4-mini-instruct"),
					DownloadAtRuntime:      true,
					ModelFileSize:          "8Gi",
					BytesPerToken:          131072,
					ModelTokenLimit:        131072,
					DiskStorageRequirement: "88Gi",
				},
				AttnType: "GQA",
			},
		},
		{
			modelRepo:   "microsoft/phi-4",
			expectFound: true,
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "phi-4",
					Architectures:          []string{"Phi3ForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "microsoft/phi-4"),
					DownloadAtRuntime:      true,
					ModelFileSize:          "28Gi",
					BytesPerToken:          204800,
					ModelTokenLimit:        16384,
					DiskStorageRequirement: "108Gi",
				},
				AttnType: "GQA",
			},
		},
		{
			modelRepo:   "google/gemma-3-4b-it",
			expectFound: true,
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "gemma-3-4b-it",
					Architectures:          []string{"Gemma3ForConditionalGeneration"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "google/gemma-3-4b-it"),
					DownloadAtRuntime:      true,
					ModelFileSize:          "9Gi",
					BytesPerToken:          139264,
					ModelTokenLimit:        131072,
					DiskStorageRequirement: "89Gi",
					ToolCallParser:         "functiongemma",
				},
				AttnType: "GQA",
			},
		},
		{
			modelRepo:   "mistralai/Mistral-7B-v0.3",
			expectFound: true,
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "mistral-7b-v0.3",
					Architectures:          []string{"MistralForCausalLM"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "mistralai/Mistral-7B-v0.3"),
					DownloadAtRuntime:      true,
					ModelFileSize:          "14Gi",
					BytesPerToken:          131072,
					ModelTokenLimit:        32768,
					DiskStorageRequirement: "94Gi",
					ToolCallParser:         "mistral",
				},
				AttnType: "GQA",
			},
		},
		{
			modelRepo:   "mistralai/Ministral-3-8B-Instruct-2512",
			expectFound: true,
			expectedParam: model.PresetParam{
				Metadata: model.Metadata{
					Name:                   "ministral-3-8b-instruct-2512",
					Architectures:          []string{"Mistral3ForConditionalGeneration"},
					ModelType:              "tfs",
					Version:                fmt.Sprintf("%s/%s", HuggingFaceWebsite, "mistralai/Ministral-3-8B-Instruct-2512"),
					DownloadAtRuntime:      true,
					ModelFileSize:          "10Gi",
					BytesPerToken:          139264,
					ModelTokenLimit:        262144,
					DiskStorageRequirement: "90Gi",
				},
				AttnType: "GQA",
			},
		},
		{
			modelRepo:   "some-org/unknown-model",
			expectFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.modelRepo, func(t *testing.T) {
			catalogData, err := os.ReadFile("../models/model_catalog.yaml")
			assert.NoError(t, err)

			gen := NewGenerator(tc.modelRepo, "")
			gen.CatalogData = catalogData
			found := gen.loadFromCatalog()
			assert.Equal(t, tc.expectFound, found)

			if !found {
				return
			}

			// Run the same pipeline that Generate() would run after loadFromCatalog
			gen.ParseModelMetadata()
			gen.FinalizeParams()

			assert.Equal(t, tc.expectedParam.Name, gen.Param.Name)
			assert.Equal(t, tc.expectedParam.Architectures, gen.Param.Architectures)
			assert.Equal(t, tc.expectedParam.ModelType, gen.Param.ModelType)
			assert.Equal(t, tc.expectedParam.Version, gen.Param.Version)
			assert.Equal(t, tc.expectedParam.DownloadAtRuntime, gen.Param.DownloadAtRuntime)
			assert.Equal(t, tc.expectedParam.Metadata.ModelFileSize, gen.Param.Metadata.ModelFileSize)
			assert.Equal(t, tc.expectedParam.Metadata.BytesPerToken, gen.Param.Metadata.BytesPerToken)
			assert.Equal(t, tc.expectedParam.Metadata.ModelTokenLimit, gen.Param.Metadata.ModelTokenLimit)
			assert.Equal(t, tc.expectedParam.Metadata.DiskStorageRequirement, gen.Param.Metadata.DiskStorageRequirement)
			assert.Equal(t, tc.expectedParam.Metadata.ToolCallParser, gen.Param.Metadata.ToolCallParser)
			assert.Equal(t, tc.expectedParam.AttnType, gen.Param.AttnType)
		})
	}
}

func TestLoadFromCatalogMistralFormats(t *testing.T) {
	catalogData, err := os.ReadFile("../models/model_catalog.yaml")
	assert.NoError(t, err)

	// Mistral catalog entries should set load_format, config_format, tokenizer_mode
	// to "mistral" in VLLM.ModelRunParams after FinalizeParams.
	mistralRepos := []string{
		"mistralai/Mistral-7B-v0.3",
		"mistralai/Mistral-7B-Instruct-v0.3",
		"mistralai/Ministral-3-3B-Instruct-2512",
		"mistralai/Ministral-3-8B-Instruct-2512",
		"mistralai/Ministral-3-14B-Instruct-2512",
	}

	for _, repo := range mistralRepos {
		t.Run(repo, func(t *testing.T) {
			gen := NewGenerator(repo, "")
			gen.CatalogData = catalogData
			found := gen.loadFromCatalog()
			assert.True(t, found)

			gen.ParseModelMetadata()
			gen.FinalizeParams()

			assert.Equal(t, "mistral", gen.Param.VLLM.ModelRunParams["load_format"])
			assert.Equal(t, "mistral", gen.Param.VLLM.ModelRunParams["config_format"])
			assert.Equal(t, "mistral", gen.Param.VLLM.ModelRunParams["tokenizer_mode"])
		})
	}

	// Non-Mistral catalog entries should have "auto" for all format fields.
	nonMistralRepos := []string{
		"google/gemma-3-4b-it",
		"google/gemma-3-27b-it",
		"microsoft/Phi-4-mini-instruct",
	}

	for _, repo := range nonMistralRepos {
		t.Run(repo, func(t *testing.T) {
			gen := NewGenerator(repo, "")
			gen.CatalogData = catalogData
			found := gen.loadFromCatalog()
			assert.True(t, found)

			gen.ParseModelMetadata()
			gen.FinalizeParams()

			assert.Equal(t, "auto", gen.Param.VLLM.ModelRunParams["load_format"])
			assert.Equal(t, "auto", gen.Param.VLLM.ModelRunParams["config_format"])
			assert.Equal(t, "auto", gen.Param.VLLM.ModelRunParams["tokenizer_mode"])
		})
	}
}

func TestSelectWeightFiles(t *testing.T) {
	cases := []struct {
		name              string
		files             []FileInfo
		expectedPaths     []string
		expectedIsMistral bool
	}{
		{
			name: "mistral consolidated weights sets IsMistralModel",
			files: []FileInfo{
				{Path: "consolidated.00.safetensors", Size: 1000},
				{Path: "consolidated.01.safetensors", Size: 2000},
				{Path: "config.json", Size: 100},
			},
			expectedPaths:     []string{"consolidated.00.safetensors", "consolidated.01.safetensors"},
			expectedIsMistral: true,
		},
		{
			name: "safetensors only does not set IsMistralModel",
			files: []FileInfo{
				{Path: "model-00001-of-00002.safetensors", Size: 5000},
				{Path: "model-00002-of-00002.safetensors", Size: 5000},
				{Path: "config.json", Size: 100},
			},
			expectedPaths:     []string{"model-00001-of-00002.safetensors", "model-00002-of-00002.safetensors"},
			expectedIsMistral: false,
		},
		{
			name: "bin files only does not set IsMistralModel",
			files: []FileInfo{
				{Path: "pytorch_model-00001-of-00002.bin", Size: 5000},
				{Path: "pytorch_model-00002-of-00002.bin", Size: 5000},
			},
			expectedPaths:     []string{"pytorch_model-00001-of-00002.bin", "pytorch_model-00002-of-00002.bin"},
			expectedIsMistral: false,
		},
		{
			name: "prefers safetensors over bin when both present",
			files: []FileInfo{
				{Path: "model.safetensors", Size: 5000},
				{Path: "pytorch_model.bin", Size: 5000},
			},
			expectedPaths:     []string{"model.safetensors"},
			expectedIsMistral: false,
		},
		{
			name: "no weight files returns empty",
			files: []FileInfo{
				{Path: "config.json", Size: 100},
				{Path: "tokenizer.json", Size: 200},
			},
			expectedPaths:     nil,
			expectedIsMistral: false,
		},
		{
			name: "mistral weights take priority over regular safetensors",
			files: []FileInfo{
				{Path: "consolidated.safetensors", Size: 1000},
				{Path: "model.safetensors", Size: 2000},
			},
			expectedPaths:     []string{"consolidated.safetensors"},
			expectedIsMistral: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := NewGenerator("test/model", "")
			selected := gen.selectWeightFiles(tc.files)

			assert.Equal(t, tc.expectedIsMistral, gen.IsMistralModel)

			var paths []string
			for _, f := range selected {
				paths = append(paths, f.Path)
			}
			assert.Equal(t, tc.expectedPaths, paths)
		})
	}
}

func TestGetString(t *testing.T) {
	config := map[string]interface{}{
		"torch_dtype": "bfloat16",
		"other_key":   "value",
		"empty_key":   "",
		"non_string":  42,
	}

	// First matching key wins
	assert.Equal(t, "bfloat16", getString(config, []string{"torch_dtype", "dtype"}))
	// Falls back to second key
	assert.Equal(t, "value", getString(config, []string{"missing", "other_key"}))
	// No match returns empty string
	assert.Equal(t, "", getString(config, []string{"no_such_key"}))
	// Skips empty string values
	assert.Equal(t, "", getString(config, []string{"empty_key"}))
	// Skips non-string values
	assert.Equal(t, "", getString(config, []string{"non_string"}))
}

func TestCalculateKVCacheTokenSizeWithDType(t *testing.T) {
	// Base GQA config: 32 layers, 32 attn heads, 8 kv heads, hidden=4096
	// headDim = 4096/32 = 128
	// elementsPerToken = 2 * 8 * 128 = 2048
	// totalElements = 2048 * 32 = 65536
	baseConfig := map[string]interface{}{
		"hidden_size":         4096,
		"num_hidden_layers":   32,
		"num_attention_heads": 32,
		"num_key_value_heads": 8,
	}

	cases := []struct {
		name             string
		dtype            string
		expectedBPT      int
		expectedAttnType string
	}{
		{"bfloat16 uses 2 bytes", "bfloat16", 65536 * 2, "GQA"},
		{"float16 uses 2 bytes", "float16", 65536 * 2, "GQA"},
		{"float32 uses 4 bytes", "float32", 65536 * 4, "GQA"},
		{"fp8 uses 1 byte", "fp8", 65536 * 1, "GQA"},
		{"int8 uses 1 byte", "int8", 65536 * 1, "GQA"},
		{"unknown dtype defaults to 2 bytes", "weird_type", 65536 * 2, "GQA"},
		{"empty dtype defaults to 2 bytes", "", 65536 * 2, "GQA"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := NewGenerator("test/model", "")
			gen.ModelConfig = baseConfig
			gen.Param.Metadata.DType = tc.dtype

			bpt, attnType := gen.calculateKVCacheTokenSize()
			assert.Equal(t, tc.expectedBPT, bpt)
			assert.Equal(t, tc.expectedAttnType, attnType)
		})
	}
}

func TestDTypeBytesMapCompleteness(t *testing.T) {
	// Ensure all dtypes used in model_catalog.yaml are present in dtypeBytesMap
	catalogData, err := os.ReadFile("../models/model_catalog.yaml")
	assert.NoError(t, err)

	var catalog ModelCatalog
	err = yaml.Unmarshal(catalogData, &catalog)
	assert.NoError(t, err)

	for _, m := range catalog.Models {
		if m.DType == "" {
			continue
		}
		_, ok := dtypeBytesMap[m.DType]
		assert.True(t, ok, "dtype %q (from model %s) not found in dtypeBytesMap", m.DType, m.Name)
	}
}

func TestLoadFromCatalogDType(t *testing.T) {
	catalogData, err := os.ReadFile("../models/model_catalog.yaml")
	assert.NoError(t, err)

	// Pick a model known to have dtype: bfloat16 in the catalog
	gen := NewGenerator("microsoft/phi-4", "")
	gen.CatalogData = catalogData
	found := gen.loadFromCatalog()
	assert.True(t, found)

	gen.ParseModelMetadata()
	gen.FinalizeParams()

	// DType should be populated from the catalog entry
	assert.Equal(t, "bfloat16", gen.Param.Metadata.DType, "loadFromCatalog should propagate DType to Param.Metadata.DType")
}
