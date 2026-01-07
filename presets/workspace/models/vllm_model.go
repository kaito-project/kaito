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
	"time"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

var (
	//go:embed supported_models_best_effort.yaml
	vLLMModelsYAML []byte
)

// VLLMCatalog is a struct that holds a list of supported models parsed
// from presets/workspace/models/supported_models_best_effort.yaml. The YAML file is
// considered the source of truth for the model metadata, and any
// information in the YAML file should not be hardcoded in the codebase.
type VLLMCatalog struct {
	Models []model.Metadata `yaml:"models,omitempty"`
}

func init() {
	vLLMCatalog := VLLMCatalog{}
	utilruntime.Must(yaml.Unmarshal(vLLMModelsYAML, &vLLMCatalog))

	// register all VLLM models
	for _, m := range vLLMCatalog.Models {
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
