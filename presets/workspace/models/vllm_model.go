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
	//go:embed model_catalog.yaml
	modelCatalogYAML []byte
)

// registerModel registers a HuggingFace model with the given ID and parameters
// into the model registry and returns the registered model. If param is nil,
// it returns nil and does not register a model.
func registerModel(hfModelCardID string, param *model.PresetParam) model.Model {
	if param == nil {
		return nil
	}

	model := &vLLMCompatibleModel{
		model:              param.Metadata,
		generatedRunParams: param.VLLM.ModelRunParams,
	}
	r := &plugin.Registration{
		Name:     hfModelCardID,
		Instance: model,
	}
	klog.InfoS("Registering VLLM-compatible model", "model", hfModelCardID, "metadata", param.Metadata)
	plugin.KaitoModelRegister.Register(r)
	return model
}

// GetModelByNameWithToken returns a vLLM-compatible model for the given modelName using
// a pre-resolved access token. Unlike GetModelByName, this function does not perform any
// Kubernetes Secret lookups; the caller is responsible for obtaining the token beforehand.
// Pass an empty string for token when working with public models that require no authentication.
func GetModelByNameWithToken(ctx context.Context, modelName, token string) (model.Model, error) {
	modelName = strings.ToLower(modelName)
	// Redirect legacy preset names (e.g. "phi-4") to their full HuggingFace
	// model ID (e.g. "microsoft/phi-4").
	if hfName, ok := plugin.LegacyBuiltinToCatalog[modelName]; ok {
		modelName = hfName
	}
	if m := plugin.KaitoModelRegister.MustGet(modelName); m != nil {
		return m, nil
	}
	if strings.Contains(modelName, "/") {
		return generateHuggingFaceModel(modelName, token)
	}
	return nil, fmt.Errorf("model is not registered: %s", modelName)
}

// GetModelByName returns a vLLM-compatible model for the given modelName.
// If the modelName contains a "/", it fetches an access token from the
// Kubernetes Secret identified by secretName and secretNamespace,
// then generates a preset for the corresponding HuggingFace model.
// Prefer GetModelByNameWithToken when the token has already been resolved by the caller.
func GetModelByName(ctx context.Context, modelName, secretName, secretNamespace string, kubeClient client.Client) (model.Model, error) {
	modelName = strings.ToLower(modelName)
	// Redirect legacy preset names (e.g. "phi-4") to their full HuggingFace
	// model ID (e.g. "microsoft/phi-4").
	if hfName, ok := plugin.LegacyBuiltinToCatalog[modelName]; ok {
		modelName = hfName
	}
	if m := plugin.KaitoModelRegister.MustGet(modelName); m != nil {
		return m, nil
	}
	if !strings.Contains(modelName, "/") {
		return nil, fmt.Errorf("model is not registered: %s", modelName)
	}
	klog.InfoS("Generating VLLM model preset for HuggingFace model", "model", modelName, "secretName", secretName, "secretNamespace", secretNamespace)
	token, err := GetHFTokenFromSecret(ctx, kubeClient, secretName, secretNamespace)
	if err != nil {
		// only log the error here since token may not be required for public models
		klog.ErrorS(err, "failed to get huggingface token from secret", "secretName", secretName, "secretNamespace", secretNamespace)
	}
	return generateHuggingFaceModel(modelName, token)
}

// generateHuggingFaceModel generates or retrieves a vLLM preset for modelName (which must
// contain a "/") using the provided token.
func generateHuggingFaceModel(modelName, token string) (model.Model, error) {
	param, err := generator.GeneratePreset(modelName, token, modelCatalogYAML)
	if err != nil {
		return nil, err
	}
	// check whether the model is in the supported model architecture list
	if len(param.Metadata.Architectures) == 0 {
		klog.InfoS("Model architecture not specified, assuming supported by VLLM", "model", modelName)
		return registerModel(modelName, param), nil
	}

	for _, arch := range param.Metadata.Architectures {
		if vLLMModelArchSet.Has(arch) {
			return registerModel(modelName, param), nil
		}
	}

	return nil, fmt.Errorf("unsupported model architecture for %s: %s", modelName, strings.Join(param.Metadata.Architectures, ", "))
}

type vLLMCompatibleModel struct {
	model              model.Metadata
	generatedRunParams map[string]string // vLLM run params produced by the generator
}

func (m *vLLMCompatibleModel) GetInferenceParameters() *model.PresetParam {
	metaData := &model.Metadata{
		Name:                 m.model.Name,
		ModelType:            "text-generation",
		Version:              m.model.Version,
		Runtime:              "tfs",
		DownloadAtRuntime:    true,
		DownloadAuthRequired: m.model.DownloadAuthRequired,
		Architectures:        m.model.Architectures,
		QuantMethod:          m.model.QuantMethod,
		QuantBits:            m.model.QuantBits,
	}

	runParamsVLLM := make(map[string]string)
	for k, v := range m.generatedRunParams {
		runParamsVLLM[k] = v
	}
	// Apply defaults for keys the generator doesn't set.
	if _, ok := runParamsVLLM["trust-remote-code"]; !ok {
		runParamsVLLM["trust-remote-code"] = ""
	}

	// For quantized models, let vLLM auto-detect the optimal dtype
	// TODO: test if we can always set dtype to "auto"
	if _, ok := runParamsVLLM["dtype"]; !ok {
		if m.model.QuantMethod != "" {
			runParamsVLLM["dtype"] = "auto"
		} else if m.model.DType != "" {
			runParamsVLLM["dtype"] = m.model.DType
		} else {
			runParamsVLLM["dtype"] = "bfloat16"
		}
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

	vllmParam := model.VLLMParam{
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            metaData.Name,
		ModelRunParams:       runParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	}

	tfsParam := TransformerInferenceParameters[m.model.Name]
	tfsParam.ModelName = metaData.Name

	presetParam := &model.PresetParam{
		Metadata:                *metaData,
		TotalSafeTensorFileSize: m.model.ModelFileSize,
		DiskStorageRequirement:  m.model.DiskStorageRequirement,
		BytesPerToken:           m.model.BytesPerToken,
		ModelTokenLimit:         m.model.ModelTokenLimit,
		RuntimeParam: model.RuntimeParam{
			Transformers: tfsParam,
			VLLM:         vllmParam,
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

	return presetParam
}

func (m *vLLMCompatibleModel) GetTuningParameters() *model.PresetParam {
	tc, ok := TransformerTuningParameters[m.model.Name]
	if !ok {
		return nil
	}
	return &model.PresetParam{
		Metadata:                      MustGet(m.model.Name),
		DiskStorageRequirement:        tc.DiskStorageRequirement,
		GPUCountRequirement:           tc.GPUCountRequirement,
		TotalSafeTensorFileSize:       tc.TotalSafeTensorFileSize,
		ModelTokenLimit:               tc.ModelTokenLimit,
		BytesPerToken:                 tc.BytesPerToken,
		TuningPerGPUMemoryRequirement: tc.TuningPerGPUMemoryRequirement,
		ReadinessTimeout:              tc.ReadinessTimeout,
		RuntimeParam: model.RuntimeParam{
			Transformers: tc.Transformers,
		},
	}
}

func (*vLLMCompatibleModel) SupportDistributedInference() bool {
	return true
}

func (m *vLLMCompatibleModel) SupportTuning() bool {
	_, ok := TransformerTuningParameters[m.model.Name]
	return ok
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
