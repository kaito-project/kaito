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

package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestVLLMCompatibleModel_GetInferenceParameters(t *testing.T) {
	tests := []struct {
		name          string
		model         model.Metadata
		expectedName  string
		expectedDType string
		checkParams   func(t *testing.T, params *model.PresetParam)
	}{
		{
			name: "basic model with default dtype",
			model: model.Metadata{
				Name:                   "test-model",
				Version:                "https://huggingface.co/test/model",
				ModelFileSize:          "2Gi",
				DiskStorageRequirement: "2Gi",
				BytesPerToken:          2,
				ModelTokenLimit:        4096,
			},
			expectedName:  "test-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "test-model", params.Metadata.Name)
				assert.Equal(t, "text-generation", params.Metadata.ModelType)
				assert.Equal(t, "https://huggingface.co/test/model", params.Metadata.Version)
				assert.Equal(t, "tfs", params.Metadata.Runtime)
				assert.True(t, params.Metadata.DownloadAtRuntime)
				assert.False(t, params.Metadata.DownloadAuthRequired)
				assert.Equal(t, "bfloat16", params.RuntimeParam.VLLM.ModelRunParams["dtype"])
				assert.Equal(t, "", params.RuntimeParam.VLLM.ModelRunParams["trust-remote-code"])
				assert.Equal(t, DefaultVLLMCommand, params.RuntimeParam.VLLM.BaseCommand)
				assert.Equal(t, time.Duration(30)*time.Minute, params.ReadinessTimeout)
			},
		},
		{
			name: "model with custom dtype",
			model: model.Metadata{
				Name:                   "custom-dtype-model",
				Version:                "https://huggingface.co/test/model",
				DType:                  "float16",
				ModelFileSize:          "2Gi",
				DiskStorageRequirement: "4Gi",
			},
			expectedName:  "custom-dtype-model",
			expectedDType: "float16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "float16", params.RuntimeParam.VLLM.ModelRunParams["dtype"])
			},
		},
		{
			name: "model with tool call parser",
			model: model.Metadata{
				Name:           "tool-model",
				Version:        "https://huggingface.co/test/model",
				ToolCallParser: "hermes",
				ModelFileSize:  "2Gi",
			},
			expectedName:  "tool-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "hermes", params.RuntimeParam.VLLM.ModelRunParams["tool-call-parser"])
				assert.Equal(t, "", params.RuntimeParam.VLLM.ModelRunParams["enable-auto-tool-choice"])
			},
		},
		{
			name: "model with chat template",
			model: model.Metadata{
				Name:          "chat-model",
				Version:       "https://huggingface.co/test/model",
				ChatTemplate:  "template.jinja",
				ModelFileSize: "2Gi",
			},
			expectedName:  "chat-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "/workspace/chat_templates/template.jinja", params.RuntimeParam.VLLM.ModelRunParams["chat-template"])
			},
		},
		{
			name: "model with allow remote files",
			model: model.Metadata{
				Name:             "remote-model",
				Version:          "https://huggingface.co/test/model",
				AllowRemoteFiles: true,
				ModelFileSize:    "2Gi",
			},
			expectedName:  "remote-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "", params.RuntimeParam.VLLM.ModelRunParams["allow-remote-files"])
			},
		},
		{
			name: "model with reasoning parser",
			model: model.Metadata{
				Name:            "reasoning-model",
				Version:         "https://huggingface.co/test/model",
				ReasoningParser: "qwq",
				ModelFileSize:   "2Gi",
			},
			expectedName:  "reasoning-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "qwq", params.RuntimeParam.VLLM.ModelRunParams["reasoning-parser"])
			},
		},
		{
			name: "model with download auth required",
			model: model.Metadata{
				Name:                 "auth-model",
				Version:              "https://huggingface.co/test/model",
				DownloadAuthRequired: true,
				ModelFileSize:        "2Gi",
			},
			expectedName:  "auth-model",
			expectedDType: "bfloat16",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.True(t, params.Metadata.DownloadAuthRequired)
			},
		},
		{
			name: "model with all options",
			model: model.Metadata{
				Name:                   "full-model",
				Version:                "https://huggingface.co/test/model",
				DType:                  "float32",
				ToolCallParser:         "mistral",
				ChatTemplate:           "custom.jinja",
				AllowRemoteFiles:       true,
				ReasoningParser:        "custom-parser",
				DownloadAuthRequired:   true,
				ModelFileSize:          "2Gi",
				DiskStorageRequirement: "8Gi",
				BytesPerToken:          4,
				ModelTokenLimit:        8192,
			},
			expectedName:  "full-model",
			expectedDType: "float32",
			checkParams: func(t *testing.T, params *model.PresetParam) {
				assert.Equal(t, "float32", params.RuntimeParam.VLLM.ModelRunParams["dtype"])
				assert.Equal(t, "mistral", params.RuntimeParam.VLLM.ModelRunParams["tool-call-parser"])
				assert.Equal(t, "", params.RuntimeParam.VLLM.ModelRunParams["enable-auto-tool-choice"])
				assert.Equal(t, "/workspace/chat_templates/custom.jinja", params.RuntimeParam.VLLM.ModelRunParams["chat-template"])
				assert.Equal(t, "", params.RuntimeParam.VLLM.ModelRunParams["allow-remote-files"])
				assert.Equal(t, "custom-parser", params.RuntimeParam.VLLM.ModelRunParams["reasoning-parser"])
				assert.True(t, params.Metadata.DownloadAuthRequired)
				assert.Equal(t, "2Gi", params.TotalSafeTensorFileSize)
				assert.Equal(t, "8Gi", params.DiskStorageRequirement)
				assert.Equal(t, 4, params.BytesPerToken)
				assert.Equal(t, 8192, params.ModelTokenLimit)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vLLMCompatibleModel{
				model: tt.model,
			}

			params := m.GetInferenceParameters()

			assert.NotNil(t, params)
			assert.Equal(t, tt.expectedName, params.RuntimeParam.VLLM.ModelName)
			tt.checkParams(t, params)
		})
	}
}

func TestVLLMCompatibleModel_GetTuningParameters(t *testing.T) {
	m := &vLLMCompatibleModel{}
	params := m.GetTuningParameters()
	assert.Nil(t, params)
}

func TestVLLMCompatibleModel_SupportDistributedInference(t *testing.T) {
	m := &vLLMCompatibleModel{}
	assert.True(t, m.SupportDistributedInference())
}

func TestVLLMCompatibleModel_SupportTuning(t *testing.T) {
	m := &vLLMCompatibleModel{}
	assert.False(t, m.SupportTuning())
}
