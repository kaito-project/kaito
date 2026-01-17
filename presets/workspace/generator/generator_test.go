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
					Name:                 "phi-4-mini-instruct",
					ModelType:            "tfs",
					Version:              "0.0.1",
					DownloadAtRuntime:    true,
					DownloadAuthRequired: false,
					ModelFileSize:        "8Gi",
				},
				BytesPerToken:          131072,
				ModelTokenLimit:        131072,
				DiskStorageRequirement: "58Gi", // 8 + 50
				AttnType:               "GQA",
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
					Name:                 "falcon-7b-instruct",
					ModelType:            "tfs",
					Version:              "0.0.1",
					DownloadAtRuntime:    true,
					DownloadAuthRequired: false,
					ModelFileSize:        "14Gi", // Python test expects 27Gi due to double counting (bin+safetensors). We fix this to use safetensors only.
				},
				BytesPerToken:          8192,
				ModelTokenLimit:        2048,
				DiskStorageRequirement: "64Gi", // 14 + 50
				AttnType:               "MQA",
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
					Name:                 "ministral-3-8b-instruct-2512",
					ModelType:            "tfs",
					Version:              "0.0.1",
					DownloadAtRuntime:    true,
					DownloadAuthRequired: false,
					ModelFileSize:        "10Gi",
				},
				BytesPerToken:          139264,
				ModelTokenLimit:        262144,
				DiskStorageRequirement: "60Gi", // 10 + 50
				AttnType:               "GQA",
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
					Name:                 "mistral-large-3-675b-instruct-2512",
					ModelType:            "tfs",
					Version:              "0.0.1",
					DownloadAtRuntime:    true,
					DownloadAuthRequired: false,
					ModelFileSize:        "635Gi",
				},
				BytesPerToken:          70272,
				ModelTokenLimit:        294912,
				DiskStorageRequirement: "685Gi", // 635 + 50
				AttnType:               "MLA",
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
	}

	for _, tc := range cases {
		t.Run(tc.modelRepo, func(t *testing.T) {
			param, err := GeneratePreset(tc.modelRepo, "")
			assert.NoError(t, err)
			assert.NotNil(t, param)

			// Metadata checks
			assert.Equal(t, tc.expectedParam.Name, param.Name)
			assert.Equal(t, tc.expectedParam.ModelType, param.ModelType)
			assert.Equal(t, tc.expectedParam.Version, param.Version)
			assert.Equal(t, tc.expectedParam.DownloadAtRuntime, param.DownloadAtRuntime)
			assert.Equal(t, tc.expectedParam.DownloadAuthRequired, param.DownloadAuthRequired)
			assert.Equal(t, tc.expectedParam.ModelFileSize, param.ModelFileSize)
			assert.Equal(t, tc.expectedParam.BytesPerToken, param.BytesPerToken)
			assert.Equal(t, tc.expectedParam.ModelTokenLimit, param.ModelTokenLimit)

			// Struct fields
			assert.Equal(t, tc.expectedParam.DiskStorageRequirement, param.DiskStorageRequirement)
			assert.Equal(t, tc.expectedParam.AttnType, param.AttnType)

			// VLLM checks
			assert.Equal(t, tc.expectedVLLM.ModelName, param.VLLM.ModelName)
			assert.Equal(t, tc.expectedVLLM.DisallowLoRA, param.VLLM.DisallowLoRA)
			assert.Equal(t, tc.expectedVLLM.ModelRunParams, param.VLLM.ModelRunParams)
		})
	}
}
