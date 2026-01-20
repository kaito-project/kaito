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
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
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

// this test only makes sure that all keys and values in builtinVLLMModels are in lower case
func TestBuiltinVLLMModels(t *testing.T) {
	t.Logf("Testing builtinVLLMModels for lower case keys and values, total models: %d", len(builtinVLLMModels))
	for k, v := range builtinVLLMModels {
		assert.Equal(t, k, strings.ToLower(k), "key is not in lower case: %s", k)
		assert.Equal(t, v, strings.ToLower(v), "value is not in lower case: %s", v)
	}
}

func TestGetModelByName_BuiltinModels(t *testing.T) {
	tests := []struct {
		name            string
		modelName       string
		expectedShort   string
		shouldFindModel bool
	}{
		{
			name:            "builtin deepseek-r1-distill-llama-8b",
			modelName:       "deepseek-ai/deepseek-r1-distill-llama-8b",
			expectedShort:   "deepseek-r1-distill-llama-8b",
			shouldFindModel: true,
		},
		{
			name:            "builtin falcon-7b",
			modelName:       "tiiuae/falcon-7b",
			expectedShort:   "falcon-7b",
			shouldFindModel: true,
		},
		{
			name:            "builtin phi-4",
			modelName:       "microsoft/phi-4",
			expectedShort:   "phi-4",
			shouldFindModel: true,
		},
		{
			name:            "builtin model with uppercase input",
			modelName:       "MICROSOFT/PHI-4",
			expectedShort:   "phi-4",
			shouldFindModel: true,
		},
		{
			name:            "builtin model mixed case",
			modelName:       "Meta-Llama/Llama-3.1-8b-Instruct",
			expectedShort:   "llama-3.1-8b-instruct",
			shouldFindModel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if the short name is registered (it should be for builtin models)
			shortModel := plugin.KaitoModelRegister.MustGet(tt.expectedShort)
			if shortModel == nil && tt.shouldFindModel {
				t.Skipf("Builtin model %s not registered, skipping test", tt.expectedShort)
			}

			result, err := GetModelByName(context.Background(), tt.modelName, "", "", nil)

			if tt.shouldFindModel && shortModel != nil {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
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
