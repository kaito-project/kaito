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
	_ "embed"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/generator"
)

var (
	// builtinVLLMModels is the mapping of built-in VLLM model names to their preset names
	// make sure all key and values are in lower case
	builtinVLLMModels = map[string]string{
		"deepseek-ai/deepseek-r1-distill-llama-8b":     "deepseek-r1-distill-llama-8b",
		"deepseek-ai/deepseek-r1-distill-qwen-14b":     "deepseek-r1-distill-qwen-14b",
		"deepseek-ai/deepseek-r1-0528":                 "deepseek-r1-0528",
		"deepseek-ai/deepseek-v3-0324":                 "deepseek-v3-0324",
		"tiiuae/falcon-7b":                             "falcon-7b",
		"tiiuae/falcon-7b-instruct":                    "falcon-7b-instruct",
		"tiiuae/falcon-40b":                            "falcon-40b",
		"tiiuae/falcon-40b-instruct":                   "falcon-40b-instruct",
		"google/gemma-3-4b-it":                         "gemma-3-4b-instruct",
		"google/gemma-3-27b-it":                        "gemma-3-27b-instruct",
		"openai/gpt-oss-20b":                           "gpt-oss-20b",
		"openai/gpt-oss-120b":                          "gpt-oss-120b",
		"meta-llama/llama-3.1-8b-instruct":             "llama-3.1-8b-instruct",
		"meta-llama/llama-3.3-70b-instruct":            "llama-3.3-70b-instruct",
		"mistralai/mistral-7b-v0.3":                    "mistral-7b",
		"mistralai/mistral-7b-instruct-v0.3":           "mistral-7b-instruct",
		"mistralai/ministral-3-3b-instruct-2512":       "ministral-3-3b-instruct",
		"mistralai/ministral-3-8b-instruct-2512":       "ministral-3-8b-instruct",
		"mistralai/ministral-3-14b-instruct-2512":      "ministral-3-14b-instruct",
		"mistralai/mistral-large-3-675b-instruct-2512": "mistral-large-3-675b-instruct",
		"microsoft/phi-3-mini-4k-instruct":             "phi-3-mini-4k-instruct",
		"microsoft/phi-3-mini-128k-instruct":           "phi-3-mini-128k-instruct",
		"microsoft/phi-3-medium-4k-instruct":           "phi-3-medium-4k-instruct",
		"microsoft/phi-3-medium-128k-instruct":         "phi-3-medium-128k-instruct",
		"microsoft/phi-3.5-mini-instruct":              "phi-3.5-mini-instruct",
		"microsoft/phi-4":                              "phi-4",
		"microsoft/phi-4-mini-instruct":                "phi-4-mini-instruct",
		"qwen/qwen2.5-coder-7b-instruct":               "qwen2.5-coder-7b-instruct",
		"qwen/qwen2.5-coder-32b-instruct":              "qwen2.5-coder-32b-instruct",
	}
)

// registerModel registers a HuggingFace model with the given ID and parameters
// into the model registry and returns the registered model. If param is nil,
// it returns nil and does not register a model.
func registerModel(hfModelCardID string, param *model.PresetParam) model.Model {
	if param == nil {
		return nil
	}

	model := &vLLMCompatibleModel{model: param.Metadata}
	r := &plugin.Registration{
		Name:     hfModelCardID,
		Instance: model,
	}
	klog.InfoS("Registering VLLM-compatible model", "model", hfModelCardID, "metadata", param.Metadata)
	plugin.KaitoModelRegister.Register(r)
	return model
}

// GetModelByName returns a vLLM-compatible model for the given modelName.
// It first looks up an existing registration in the KaitoModelRegister. If the
// model is not already registered and modelName contains a "/", it attempts to
// generate a preset for the corresponding HuggingFace model by optionally
// retrieving an access token from the Kubernetes Secret identified by
// secretName and secretNamespace using kubeClient and ctx.
//
// The returned model.Model represents the registered or newly generated model.
// If preset generation fails, or if the modelName does not correspond to a
// registered or generatable model, this method returns an error instead of panicking.
// Callers should ensure that modelName is valid and handle the error accordingly.
func GetModelByName(ctx context.Context, modelName, secretName, secretNamespace string, kubeClient client.Client) (model.Model, error) {
	modelName = strings.ToLower(modelName)
	model := plugin.KaitoModelRegister.MustGet(modelName)
	if model != nil {
		return model, nil
	}

	// if name contains "/", get model data from HuggingFace
	if strings.Contains(modelName, "/") {
		if builtinModelName, ok := builtinVLLMModels[modelName]; ok {
			klog.InfoS("Using built-in VLLM model preset", "model", modelName, "builtinModelName", builtinModelName)
			return plugin.KaitoModelRegister.MustGet(builtinModelName), nil
		}

		klog.InfoS("Generating VLLM model preset for HuggingFace model", "model", modelName, "secretName", secretName, "secretNamespace", secretNamespace)
		token, err := GetHFTokenFromSecret(ctx, kubeClient, secretName, secretNamespace)
		if err != nil {
			// only log the error here since token may not be required for public models
			klog.ErrorS(err, "failed to get huggingface token from secret", "secretName", secretName, "secretNamespace", secretNamespace)
		}
		param, err := generator.GeneratePreset(modelName, token)
		if err != nil {
			return nil, err
		}
		// check whether the model is in the supported model architecture list
		if len(param.Metadata.Architectures) == 0 {
			klog.InfoS("Model architecture not specified, assuming supported by VLLM", "model", modelName)
			return registerModel(modelName, param), nil
		}

		for _, arch := range param.Metadata.Architectures {
			if _, ok := vLLMModelArchMap[arch]; ok {
				return registerModel(modelName, param), nil
			}
		}

		return nil, fmt.Errorf("unsupported model architecture for %s: %s", modelName, strings.Join(param.Metadata.Architectures, ", "))
	}
	return nil, fmt.Errorf("model is not registered: %s", modelName)
}

type vLLMCompatibleModel struct {
	model model.Metadata
}

func (m *vLLMCompatibleModel) GetInferenceParameters() *model.PresetParam {
	metaData := &model.Metadata{
		Name:                 m.model.Name,
		ModelType:            "text-generation",
		Version:              m.model.Version,
		Runtime:              "tfs",
		DownloadAtRuntime:    true,
		DownloadAuthRequired: m.model.DownloadAuthRequired,
	}

	runParamsVLLM := map[string]string{
		"trust-remote-code": "",
	}
	if m.model.DType != "" {
		runParamsVLLM["dtype"] = m.model.DType
	} else {
		runParamsVLLM["dtype"] = "bfloat16"
	}

	if m.model.ToolCallParser != "" {
		runParamsVLLM["tool-call-parser"] = m.model.ToolCallParser
		runParamsVLLM["enable-auto-tool-choice"] = ""
	}
	if m.model.ChatTemplate != "" {
		runParamsVLLM["chat-template"] = "/workspace/chat_templates/" + m.model.ChatTemplate
	}
	if m.model.AllowRemoteFiles {
		runParamsVLLM["allow-remote-files"] = ""
	}
	if m.model.ReasoningParser != "" {
		runParamsVLLM["reasoning-parser"] = m.model.ReasoningParser
	}

	presetParam := &model.PresetParam{
		Metadata:                *metaData,
		TotalSafeTensorFileSize: m.model.ModelFileSize,
		DiskStorageRequirement:  m.model.DiskStorageRequirement,
		BytesPerToken:           m.model.BytesPerToken,
		ModelTokenLimit:         m.model.ModelTokenLimit,
		RuntimeParam: model.RuntimeParam{
			VLLM: model.VLLMParam{
				BaseCommand:          DefaultVLLMCommand,
				ModelName:            metaData.Name,
				ModelRunParams:       runParamsVLLM,
				RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

	return presetParam
}

func (*vLLMCompatibleModel) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*vLLMCompatibleModel) SupportDistributedInference() bool {
	return true
}

func (*vLLMCompatibleModel) SupportTuning() bool {
	return false
}

// GetHFTokenFromSecret retrieves the HuggingFace token from a Kubernetes secret.
// If secretName is empty, it returns an empty string without error.
// If secretNamespace is empty, it defaults to "default".
// An error is returned if kubeClient is nil, the secret cannot be retrieved,
// or the HF_TOKEN key is not present in the secret data.
func GetHFTokenFromSecret(ctx context.Context, kubeClient client.Client, secretName, secretNamespace string) (string, error) {
	if secretName == "" {
		return "", nil
	}

	if kubeClient == nil {
		return "", fmt.Errorf("kubeClient is nil")
	}

	if secretNamespace == "" {
		secretNamespace = "default"
	}

	secret := corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: secretNamespace}, &secret); err != nil {
		return "", fmt.Errorf("failed to get secret: %s in namespace: %s, error: %w", secretName, secretNamespace, err)
	}

	tokenBytes, ok := secret.Data["HF_TOKEN"]
	if !ok {
		return "", fmt.Errorf("HF_TOKEN not found in secret: %s in namespace: %s", secretName, secretNamespace)
	}

	return string(tokenBytes), nil
}
