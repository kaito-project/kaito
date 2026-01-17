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

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/generator"
)

var (
	//go:embed supported_models_best_effort.yaml
	vLLMModelsYAML         []byte
	KaitoVLLMModelRegister = vLLMCatalog{}
)

// vLLMCatalog is a struct that holds a list of supported models parsed
// from presets/workspace/models/supported_models_best_effort.yaml. The YAML file is
// considered the source of truth for the model metadata, and any
// information in the YAML file should not be hardcoded in the codebase.
type vLLMCatalog struct {
	Models []model.Metadata `yaml:"models,omitempty"`
}

// RegisterModel registers a HuggingFace model with the given ID and parameters
// into the model registry and returns the registered model. If param is nil,
// it returns nil and does not register a model.
func (m *vLLMCatalog) RegisterModel(hfModelCardID string, param *model.PresetParam) model.Model {
	if param == nil {
		return nil
	}

	model := &vLLMCompatibleModel{model: param.Metadata}
	r := &plugin.Registration{
		Name:     hfModelCardID,
		Instance: model,
	}
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
// registered or generatable model, this method panics instead of returning an
// error. Callers should ensure that modelName is valid and be aware of this
// panic behavior when integrating this method.
func (m *vLLMCatalog) GetModelByName(ctx context.Context, modelName, secretName, secretNamespace string, kubeClient client.Client) model.Model {
	modelName = strings.ToLower(modelName)
	model := plugin.KaitoModelRegister.MustGet(modelName)
	if model != nil {
		return model
	}

	// if name contains "/", get model data from HuggingFace
	if strings.Contains(modelName, "/") {
		klog.InfoS("Generating VLLM model preset for HuggingFace model", "model", modelName, "secretName", secretName, "secretNamespace", secretNamespace)
		token, err := GetHFTokenFromSecret(ctx, kubeClient, secretName, secretNamespace)
		if err != nil {
			klog.ErrorS(err, "Failed to get HF token from secret", "secretName", secretName, "secretNamespace", secretNamespace)
		}
		param, err := generator.GeneratePreset(modelName, token)
		if err != nil {
			panic("could not generate preset for model: " + modelName + ", error: " + err.Error())
		}
		return m.RegisterModel(modelName, param)
	}
	panic("model is not registered: " + modelName)
}

func init() {
	utilruntime.Must(yaml.Unmarshal(vLLMModelsYAML, &KaitoVLLMModelRegister))

	// register all VLLM models
	for _, m := range KaitoVLLMModelRegister.Models {
		utilruntime.Must(m.Validate())
		plugin.KaitoModelRegister.Register(&plugin.Registration{
			Name:     m.Name,
			Instance: &vLLMCompatibleModel{model: m},
		})
		klog.InfoS("Registered VLLM model preset", "model", m.Name)
	}
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
