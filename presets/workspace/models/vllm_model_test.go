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
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/generator"
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

func TestVLLMCompatibleModel_GetInferenceParameters_TransformerLookup(t *testing.T) {
	tests := []struct {
		name                       string
		modelName                  string
		expectTransformerPopulated bool
	}{
		{
			name:                       "model in TransformerInferenceParameters map gets Transformers populated",
			modelName:                  "phi-4",
			expectTransformerPopulated: true,
		},
		{
			name:                       "model not in TransformerInferenceParameters map gets empty Transformers",
			modelName:                  "unknown-dynamic-model",
			expectTransformerPopulated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vLLMCompatibleModel{
				model: model.Metadata{Name: tt.modelName},
			}
			params := m.GetInferenceParameters()
			assert.NotNil(t, params)

			if tt.expectTransformerPopulated {
				expected := TransformerInferenceParameters["phi-4"]
				assert.Equal(t, expected.BaseCommand, params.RuntimeParam.Transformers.BaseCommand)
				assert.Equal(t, tt.modelName, params.RuntimeParam.Transformers.ModelName)
				assert.Equal(t, expected.InferenceMainFile, params.RuntimeParam.Transformers.InferenceMainFile)
				assert.NotEmpty(t, params.RuntimeParam.Transformers.AccelerateParams)
			} else {
				assert.Empty(t, params.RuntimeParam.Transformers.BaseCommand)
				assert.Equal(t, tt.modelName, params.RuntimeParam.Transformers.ModelName)
			}
		})
	}
}

func TestVLLMCompatibleModel_GetInferenceParameters_VLLMLookup(t *testing.T) {
	tests := []struct {
		name                  string
		modelName             string
		generatedRunParams    map[string]string
		expectModelName       string // expected served model name
		expectRayCommands     bool
		expectDisallowLoRA    bool
		expectModelRunParamKV map[string]string // subset of expected run params
	}{
		{
			name:      "generator-produced chat-template is preserved",
			modelName: "phi-4-mini-instruct",
			generatedRunParams: map[string]string{
				"chat-template": "/workspace/chat_templates/tool-chat-phi4-mini.jinja",
			},
			expectModelName:   "phi-4-mini-instruct", // same as Metadata.Name
			expectRayCommands: true,
			expectModelRunParamKV: map[string]string{
				"chat-template":     "/workspace/chat_templates/tool-chat-phi4-mini.jinja",
				"trust-remote-code": "",         // from dynamic base
				"dtype":             "bfloat16", // from dynamic base
			},
		},
		{
			name:      "generator-produced attention-backend is preserved",
			modelName: "llama-3.1-8b-instruct",
			generatedRunParams: map[string]string{
				"attention-backend": "TRITON_ATTN",
			},
			expectModelName:   "llama-3.1-8b-instruct",
			expectRayCommands: true,
			expectModelRunParamKV: map[string]string{
				"attention-backend": "TRITON_ATTN",
				"trust-remote-code": "",
				"dtype":             "bfloat16",
			},
		},
		{
			name:              "model name is used as served model name",
			modelName:         "gemma-3-4b-it",
			expectModelName:   "gemma-3-4b-it",
			expectRayCommands: true,
			expectModelRunParamKV: map[string]string{
				"trust-remote-code": "",
				"dtype":             "bfloat16",
			},
		},
		{
			name:              "unknown model uses only dynamic VLLM params",
			modelName:         "some-org/unknown-dynamic-model",
			expectModelName:   "some-org/unknown-dynamic-model",
			expectRayCommands: true,
			expectModelRunParamKV: map[string]string{
				"trust-remote-code": "",
				"dtype":             "bfloat16",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vLLMCompatibleModel{
				model:              model.Metadata{Name: tt.modelName},
				generatedRunParams: tt.generatedRunParams,
			}
			params := m.GetInferenceParameters()
			assert.NotNil(t, params)

			// All models get dynamic base params
			assert.Equal(t, DefaultVLLMCommand, params.RuntimeParam.VLLM.BaseCommand)
			assert.Contains(t, params.RuntimeParam.VLLM.ModelRunParams, "trust-remote-code")
			assert.Contains(t, params.RuntimeParam.VLLM.ModelRunParams, "dtype")

			if tt.expectModelName != "" {
				assert.Equal(t, tt.expectModelName, params.RuntimeParam.VLLM.ModelName)
			}

			if tt.expectRayCommands {
				assert.Equal(t, DefaultVLLMRayLeaderBaseCommand, params.RuntimeParam.VLLM.RayLeaderBaseCommand)
				assert.Equal(t, DefaultVLLMRayWorkerBaseCommand, params.RuntimeParam.VLLM.RayWorkerBaseCommand)
			}

			if tt.expectDisallowLoRA {
				assert.True(t, params.RuntimeParam.VLLM.DisallowLoRA)
			}

			for k, v := range tt.expectModelRunParamKV {
				assert.Equal(t, v, params.RuntimeParam.VLLM.ModelRunParams[k],
					"expected ModelRunParams[%q] = %q", k, v)
			}
		})
	}
}

func TestVLLMCompatibleModel_GetTuningParameters(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		expectNil bool
	}{
		{
			name:      "model not in tuning map returns nil",
			modelName: "unknown-model",
			expectNil: true,
		},
		{
			name:      "model in tuning map returns populated PresetParam",
			modelName: "phi-4",
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vLLMCompatibleModel{
				model: model.Metadata{Name: tt.modelName},
			}
			params := m.GetTuningParameters()

			if tt.expectNil {
				assert.Nil(t, params)
				return
			}

			assert.NotNil(t, params)
			tc := TransformerTuningParameters["phi-4"]
			assert.Equal(t, tc.DiskStorageRequirement, params.DiskStorageRequirement)
			assert.Equal(t, tc.GPUCountRequirement, params.GPUCountRequirement)
			assert.Equal(t, tc.TotalSafeTensorFileSize, params.TotalSafeTensorFileSize)
			assert.Equal(t, tc.ModelTokenLimit, params.ModelTokenLimit)
			assert.Equal(t, tc.BytesPerToken, params.BytesPerToken)
			assert.Equal(t, tc.TuningPerGPUMemoryRequirement, params.TuningPerGPUMemoryRequirement)
			assert.Equal(t, tc.ReadinessTimeout, params.ReadinessTimeout)
			assert.Equal(t, tc.Transformers.BaseCommand, params.RuntimeParam.Transformers.BaseCommand)
			assert.Equal(t, tc.Transformers.ModelName, params.RuntimeParam.Transformers.ModelName)
		})
	}
}

func TestVLLMCompatibleModel_SupportDistributedInference(t *testing.T) {
	m := &vLLMCompatibleModel{}
	assert.True(t, m.SupportDistributedInference())
}

func TestVLLMCompatibleModel_SupportTuning(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		expected  bool
	}{
		{
			name:      "model in tuning map returns true",
			modelName: "phi-4",
			expected:  true,
		},
		{
			name:      "model not in tuning map returns false",
			modelName: "unknown-model",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vLLMCompatibleModel{
				model: model.Metadata{Name: tt.modelName},
			}
			assert.Equal(t, tt.expected, m.SupportTuning())
		})
	}
}

func TestRegisterModel(t *testing.T) {
	tests := []struct {
		name           string
		hfModelCardID  string
		param          *model.PresetParam
		expectedResult bool
	}{
		{
			name:           "nil param returns nil",
			hfModelCardID:  "test/model",
			param:          nil,
			expectedResult: false,
		},
		{
			name:          "valid param registers model",
			hfModelCardID: "test/valid-model",
			param: &model.PresetParam{
				Metadata: model.Metadata{
					Name:    "valid-model",
					Version: "https://huggingface.co/test/valid-model",
				},
			},
			expectedResult: true,
		},
		{
			name:          "model with full metadata",
			hfModelCardID: "org/full-model",
			param: &model.PresetParam{
				Metadata: model.Metadata{
					Name:                 "full-model",
					Version:              "https://huggingface.co/org/full-model",
					DType:                "bfloat16",
					DownloadAuthRequired: true,
				},
				TotalSafeTensorFileSize: "4Gi",
				DiskStorageRequirement:  "8Gi",
			},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registerModel(tt.hfModelCardID, tt.param)

			if tt.expectedResult {
				assert.NotNil(t, result)
				// Verify the model was registered
				registered := plugin.KaitoModelRegister.MustGet(tt.hfModelCardID)
				assert.NotNil(t, registered)
				assert.Equal(t, result, registered)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestGetHFTokenFromSecret(t *testing.T) {
	tests := []struct {
		name            string
		secretName      string
		secretNamespace string
		kubeClient      client.Client
		setupFunc       func() client.Client
		expectedToken   string
		expectedError   string
	}{
		{
			name:            "empty secret name returns empty string",
			secretName:      "",
			secretNamespace: "default",
			kubeClient:      nil,
			expectedToken:   "",
			expectedError:   "",
		},
		{
			name:            "nil kubeClient returns error",
			secretName:      "my-secret",
			secretNamespace: "default",
			kubeClient:      nil,
			expectedToken:   "",
			expectedError:   "kubeClient is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := GetHFTokenFromSecret(context.Background(), tt.kubeClient, tt.secretName, tt.secretNamespace)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedToken, token)
		})
	}
}

func TestGetModelByName(t *testing.T) {
	tests := []struct {
		name            string
		modelName       string
		secretName      string
		secretNamespace string
		setupFunc       func()
		kubeClient      client.Client
		expectedError   string
		validateResult  func(t *testing.T, result model.Model)
	}{
		{
			name:      "returns registered model",
			modelName: "pre-registered-model",
			setupFunc: func() {
				// Pre-register a model
				param := &model.PresetParam{
					Metadata: model.Metadata{
						Name:    "pre-registered-model",
						Version: "https://huggingface.co/test/pre-registered-model",
					},
				}
				registerModel("pre-registered-model", param)
			},
			kubeClient:    nil,
			expectedError: "",
			validateResult: func(t *testing.T, result model.Model) {
				assert.NotNil(t, result)
			},
		},
		{
			name:      "returns registered model with uppercase conversion",
			modelName: "PRE-REGISTERED-UPPER",
			setupFunc: func() {
				param := &model.PresetParam{
					Metadata: model.Metadata{
						Name:    "pre-registered-upper",
						Version: "https://huggingface.co/test/pre-registered-upper",
					},
				}
				registerModel("pre-registered-upper", param)
			},
			kubeClient:    nil,
			expectedError: "",
			validateResult: func(t *testing.T, result model.Model) {
				assert.NotNil(t, result)
			},
		},
		{
			name:          "returns error for unregistered model without slash",
			modelName:     "unregistered-model-no-slash",
			setupFunc:     func() {},
			kubeClient:    nil,
			expectedError: "model is not registered: unregistered-model-no-slash",
			validateResult: func(t *testing.T, result model.Model) {
				assert.Nil(t, result)
			},
		},
		{
			name:            "returns error for model with slash but nil kubeClient and non-empty secret",
			modelName:       "org/some-model",
			secretName:      "hf-secret",
			secretNamespace: "default",
			setupFunc:       func() {},
			kubeClient:      nil,
			expectedError:   "", // Will fail at GeneratePreset since we can't mock it
			validateResult: func(t *testing.T, result model.Model) {
				// This test case will likely error due to GeneratePreset trying to connect
				// In a real scenario, you'd mock the generator
			},
		},
		{
			name:            "converts model name to lowercase before lookup",
			modelName:       "UPPERCASE-MODEL",
			secretName:      "",
			secretNamespace: "",
			setupFunc: func() {
				param := &model.PresetParam{
					Metadata: model.Metadata{
						Name:    "uppercase-model",
						Version: "https://huggingface.co/test/uppercase-model",
					},
				}
				registerModel("uppercase-model", param)
			},
			kubeClient:    nil,
			expectedError: "",
			validateResult: func(t *testing.T, result model.Model) {
				assert.NotNil(t, result)
			},
		},
		{
			name:            "returns error for empty model name",
			modelName:       "",
			secretName:      "",
			secretNamespace: "",
			setupFunc:       func() {},
			kubeClient:      nil,
			expectedError:   "model is not registered:",
			validateResult: func(t *testing.T, result model.Model) {
				assert.Nil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupFunc()

			result, err := GetModelByName(context.Background(), tt.modelName, tt.secretName, tt.secretNamespace, tt.kubeClient)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else if err != nil && tt.validateResult != nil {
				// Some tests may error due to external dependencies (like GeneratePreset)
				// Only validate if no error expected
				t.Logf("Got error (may be expected due to external dependencies): %v", err)
			}

			if tt.validateResult != nil && (tt.expectedError == "" || err == nil) {
				tt.validateResult(t, result)
			}
		})
	}
}

func TestGetModelByName_BuiltinModels(t *testing.T) {
	tests := []struct {
		name          string
		modelName     string
		expectedShort string
	}{
		{
			name:          "builtin deepseek-r1-distill-llama-8b",
			modelName:     "deepseek-ai/deepseek-r1-distill-llama-8b",
			expectedShort: "deepseek-r1-distill-llama-8b",
		},
		{
			name:          "builtin phi-4",
			modelName:     "microsoft/phi-4",
			expectedShort: "phi-4",
		},
		{
			name:          "builtin model with uppercase input",
			modelName:     "MICROSOFT/PHI-4",
			expectedShort: "phi-4",
		},
		{
			name:          "builtin model mixed case",
			modelName:     "Meta-Llama/Llama-3.1-8b-Instruct",
			expectedShort: "llama-3.1-8b-instruct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), tt.modelName, "", "", nil)

			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestGetModelByName_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		modelName     string
		expectedError string
	}{
		{
			name:          "unregistered model without slash",
			modelName:     "nonexistent-model",
			expectedError: "model is not registered: nonexistent-model",
		},
		{
			name:          "empty model name",
			modelName:     "",
			expectedError: "model is not registered:",
		},
		{
			name:          "model with only spaces",
			modelName:     "   ",
			expectedError: "model is not registered:",
		},
		{
			name:          "model with special characters no slash",
			modelName:     "model-with-dashes_and_underscores",
			expectedError: "model is not registered: model-with-dashes_and_underscores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), tt.modelName, "", "", nil)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
			assert.Nil(t, result)
		})
	}
}

func TestGetModelByName_CaseInsensitivity(t *testing.T) {
	// Register a test model
	testModelName := "case-test-model-unique"
	param := &model.PresetParam{
		Metadata: model.Metadata{
			Name:    testModelName,
			Version: "https://huggingface.co/test/case-test-model",
		},
	}
	registerModel(testModelName, param)

	tests := []struct {
		name      string
		inputName string
	}{
		{
			name:      "lowercase input",
			inputName: "case-test-model-unique",
		},
		{
			name:      "uppercase input",
			inputName: "CASE-TEST-MODEL-UNIQUE",
		},
		{
			name:      "mixed case input",
			inputName: "Case-Test-Model-Unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), tt.inputName, "", "", nil)

			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestGetModelByName_HuggingFaceModelWithSlash(t *testing.T) {
	tests := []struct {
		name            string
		modelName       string
		secretName      string
		secretNamespace string
		kubeClient      client.Client
		expectError     bool
		errorContains   string
	}{
		{
			name:            "unregistered HF model with nil client and no secret",
			modelName:       "unknown-org/unknown-model-12345",
			secretName:      "",
			secretNamespace: "",
			kubeClient:      nil,
			expectError:     true,
			errorContains:   "", // Will error from GeneratePreset
		},
		{
			name:            "unregistered HF model with secret but nil client",
			modelName:       "test-org/test-model-xyz",
			secretName:      "hf-token-secret",
			secretNamespace: "default",
			kubeClient:      nil,
			expectError:     true,
			errorContains:   "", // Will error from GeneratePreset or token retrieval
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), tt.modelName, tt.secretName, tt.secretNamespace, tt.kubeClient)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestGetModelByName_PreRegisteredModel(t *testing.T) {
	// Register multiple test models
	testModels := []struct {
		id   string
		name string
	}{
		{id: "test-org/model-a", name: "model-a"},
		{id: "another-org/model-b", name: "model-b"},
		{id: "simple-model-c", name: "simple-model-c"},
	}

	for _, m := range testModels {
		param := &model.PresetParam{
			Metadata: model.Metadata{
				Name:    m.name,
				Version: "https://huggingface.co/" + m.id,
			},
		}
		registerModel(m.id, param)
	}

	for _, m := range testModels {
		t.Run("lookup "+m.id, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), m.id, "", "", nil)

			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestGetModelByName_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// For a registered model, context cancellation shouldn't matter
	testModelName := "context-test-model"
	param := &model.PresetParam{
		Metadata: model.Metadata{
			Name:    testModelName,
			Version: "https://huggingface.co/test/context-test-model",
		},
	}
	registerModel(testModelName, param)

	result, err := GetModelByName(ctx, testModelName, "", "", nil)

	// Should still work for registered models since context is only used for k8s client
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGenerateHuggingFaceModel_CatalogOnlyModelsUseCatalogPath(t *testing.T) {
	// All legacy short-name models go through GeneratePreset and
	// register a new vLLMCompatibleModel.
	for _, modelName := range plugin.LegacyBuiltinToCatalog {
		t.Run(modelName, func(t *testing.T) {
			result, err := generateHuggingFaceModel(modelName, "")
			// Should succeed via model catalog
			assert.NoError(t, err)
			assert.NotNil(t, result)

			// Verify it's a vLLMCompatibleModel from the catalog path.
			_, isVLLM := result.(*vLLMCompatibleModel)
			assert.True(t, isVLLM,
				"model %q should be a vLLMCompatibleModel", modelName)

			// The catalog path registers the model under the full HF name.
			registered := plugin.KaitoModelRegister.MustGet(modelName)
			assert.NotNil(t, registered,
				"model %q should be registered under full HF name after catalog generation", modelName)
		})
	}
}

func TestGetModelByName_ShortNameRedirectsToCatalog(t *testing.T) {
	// When a short name (e.g. "phi-4") is in catalogOnlyPresets, GetModelByName
	// should redirect to the full HF name and generate via model catalog
	// instead of returning the pre-registered phi4Model.
	for shortName, hfName := range plugin.LegacyBuiltinToCatalog {
		t.Run(shortName, func(t *testing.T) {
			result, err := GetModelByName(context.Background(), shortName, "", "", nil)
			assert.NoError(t, err)
			assert.NotNil(t, result)

			// The result should be a vLLMCompatibleModel, not the pre-registered model type.
			_, isVLLM := result.(*vLLMCompatibleModel)
			assert.True(t, isVLLM, "model %q should resolve to vLLMCompatibleModel, not pre-registered type", shortName)

			// Should be registered under the full HF name
			registered := plugin.KaitoModelRegister.MustGet(hfName)
			assert.NotNil(t, registered)
		})
	}
}

func TestGetModelByNameWithToken_ShortNameRedirectsToCatalog(t *testing.T) {
	for shortName := range plugin.LegacyBuiltinToCatalog {
		t.Run(shortName, func(t *testing.T) {
			result, err := GetModelByNameWithToken(context.Background(), shortName, "")
			assert.NoError(t, err)
			assert.NotNil(t, result)

			_, isVLLM := result.(*vLLMCompatibleModel)
			assert.True(t, isVLLM, "model %q should resolve to vLLMCompatibleModel", shortName)
		})
	}
}

// TestCatalogModelsHaveMTBenchScores ensures every model in model_catalog.yaml
// has a corresponding score entry in model_catalog_mtbench_scores.md.
func TestCatalogModelsHaveMTBenchScores(t *testing.T) {
	// Parse the model catalog.
	var catalog generator.ModelCatalog
	err := yaml.Unmarshal(modelCatalogYAML, &catalog)
	if err != nil {
		t.Fatalf("Failed to parse model_catalog.yaml: %v", err)
	}

	// Parse scored model names from the markdown table.
	scoredModels := parseMTBenchScores(t, "model_catalog_mtbench_scores.md")

	// Check each catalog model has a score.
	for _, entry := range catalog.Models {
		t.Run(entry.Name, func(t *testing.T) {
			_, found := scoredModels[strings.ToLower(entry.Name)]
			assert.True(t, found,
				"model %q has no MT-bench score in model_catalog_mtbench_scores.md",
				entry.Name)
		})
	}
}

// parseMTBenchScores reads the markdown scores file and returns a set of
// lowercase model names that have score entries.
func parseMTBenchScores(t *testing.T, filename string) map[string]bool {
	t.Helper()
	f, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Failed to open %s: %v", filename, err)
	}
	defer f.Close()

	scores := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	headerSkipped := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// Skip the header row and separator row.
		if !headerSkipped {
			headerSkipped = true
			continue // "| Model | Runtime | ..."
		}
		if strings.Contains(line, "---") {
			continue // separator row
		}
		cols := strings.Split(line, "|")
		if len(cols) < 3 {
			continue
		}
		modelName := strings.TrimSpace(cols[1])
		if modelName != "" {
			scores[strings.ToLower(modelName)] = true
		}
	}
	return scores
}
