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
	_ "embed"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

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

func (m *vLLMCatalog) RegisterModel(hfModelRepoName string, param *model.PresetParam) model.Model {
	if param == nil {
		return nil
	}

	model := &VLLMCompatibleModel{model: param.Metadata}
	r := &plugin.Registration{
		Name:            param.Metadata.Name,
		HFModelRepoName: hfModelRepoName,
		Instance:        model,
	}
	plugin.KaitoModelRegister.Register(r)
	return model
}

func (m *vLLMCatalog) GetModelByName(name string) model.Model {
	name = strings.ToLower(name)
	model := plugin.KaitoModelRegister.MustGet(name)
	if model != nil {
		return model
	}

	// if name contains "/", get model data from HuggingFace
	if strings.Contains(name, "/") {
		klog.InfoS("Generating VLLM model preset for HuggingFace model", "model", name)
		// todo: add hf_token support
		param, err := generator.GeneratePreset(name, "")
		if err != nil {
			panic("could not generate preset for model: " + name + ", error: " + err.Error())
		}
		return m.RegisterModel(name, param)
	}
	panic("model is not registered: " + name)
}

func init() {
	utilruntime.Must(yaml.Unmarshal(vLLMModelsYAML, &KaitoVLLMModelRegister))

	// register all VLLM models
	for _, m := range KaitoVLLMModelRegister.Models {
		utilruntime.Must(m.Validate())
		plugin.KaitoModelRegister.Register(&plugin.Registration{
			Name:     m.Name,
			Instance: &VLLMCompatibleModel{model: m},
		})
		klog.InfoS("Registered VLLM model preset", "model", m.Name)
	}
}

type VLLMCompatibleModel struct {
	model model.Metadata
}

func (m *VLLMCompatibleModel) GetInferenceParameters() *model.PresetParam {
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

func (*VLLMCompatibleModel) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*VLLMCompatibleModel) SupportDistributedInference() bool {
	return true
}

func (*VLLMCompatibleModel) SupportTuning() bool {
	return false
}
