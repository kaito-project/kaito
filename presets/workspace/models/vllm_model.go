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

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

const DefaultVLLMCommand = "python3 /workspace/vllm/inference_api.py"

var (
	//go:embed supported_models_best_effort.yaml
	vLLMModelsYAML []byte
)

// VLLMCatalog is a struct that holds a list of supported models parsed
// from preset/workspace/models/supported_models_best_effort.yaml. The YAML file is
// considered the source of truth for the model metadata, and any
// information in the YAML file should not be hardcoded in the codebase.
type VLLMCatalog struct {
	Models []model.VLLMModel `yaml:"models,omitempty"`
}

func init() {
	vLLMCatalog := VLLMCatalog{}
	utilruntime.Must(yaml.Unmarshal(vLLMModelsYAML, &vLLMCatalog))

	// register all VLLM models
	for _, m := range vLLMCatalog.Models {
		utilruntime.Must(m.Validate())
		plugin.KaitoModelRegister.Register(&plugin.Registration{
			Name:     m.Name,
			Instance: &vllmModel{model: m},
		})
	}

}

type vllmModel struct {
	model model.VLLMModel
}

func (m *vllmModel) GetInferenceParameters() *model.PresetParam {
	metaData := &model.Metadata{
		Name:                 m.model.Name,
		ModelType:            m.model.ModelType,
		Version:              m.model.Version,
		Runtime:              m.model.Runtime,
		DownloadAtRuntime:    m.model.DownloadAtRuntime,
		DownloadAuthRequired: m.model.DownloadAuthRequired,
	}

	runParamsVLLM := map[string]string{
		"dtype": "float16",
	}

	if m.model.ToolCallParser != "" {
		runParamsVLLM["tool-call-parser"] = m.model.ToolCallParser
		runParamsVLLM["enable-auto-tool-choice"] = ""
	}
	if m.model.ChatTemplate != "" {
		runParamsVLLM["chat-template"] = m.model.ChatTemplate
	}

	presetParam := &model.PresetParam{
		Metadata:                *metaData,
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: m.model.ModelFileSizeGB,
		DiskStorageRequirement:  m.model.DiskStorageRequirement,
		BytesPerToken:           m.model.BytesPerToken,
		ModelTokenLimit:         m.model.ModelTokenLimit,
		RuntimeParam: model.RuntimeParam{
			VLLM: model.VLLMParam{
				BaseCommand:    DefaultVLLMCommand,
				ModelName:      metaData.Name,
				ModelRunParams: runParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

	return presetParam
}

func (*vllmModel) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*vllmModel) SupportDistributedInference() bool {
	return false
}

func (*vllmModel) SupportTuning() bool {
	return false
}
