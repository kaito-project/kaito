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

package phi4

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi4Model,
		Instance: &phi4A,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi4MiniInstructModel,
		Instance: &phi4MiniB,
	})
}

const (
	PresetPhi4Model             = "phi-4"
	PresetPhi4MiniInstructModel = "phi-4-mini-instruct"
)

var (
	baseCommandPresetPhiInference = "accelerate launch"
	baseCommandPresetPhiTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	phiRunParams                  = map[string]string{
		"torch_dtype":       "auto",
		"pipeline":          "text-generation",
		"trust_remote_code": "",
	}
	phiRunParamsVLLM = map[string]string{
		"dtype":                   "float16",
		"chat-template":           "/workspace/chat_templates/tool-chat-phi4-mini.jinja",
		"tool-call-parser":        "phi4_mini_json",
		"enable-auto-tool-choice": "",
	}
)

var phi4A phi4Model

type phi4Model struct{}

func (*phi4Model) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi4Model),
		DiskStorageRequirement:    "150Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "40Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "0Gi",  // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetPhiInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    phiRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetPhi4Model,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*phi4Model) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi4Model),
		DiskStorageRequirement:    "150Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "70Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "70Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}

func (*phi4Model) SupportDistributedInference() bool { return false }
func (*phi4Model) SupportTuning() bool {
	return true
}

var phi4MiniB phi4MiniInstruct

type phi4MiniInstruct struct{}

func (*phi4MiniInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi4MiniInstructModel),
		DiskStorageRequirement:    "70Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "8Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetPhiInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    phiRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetPhi4MiniInstructModel,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*phi4MiniInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi4MiniInstructModel),
		DiskStorageRequirement:    "70Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "72Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "72Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}

func (*phi4MiniInstruct) SupportDistributedInference() bool { return false }
func (*phi4MiniInstruct) SupportTuning() bool {
	return true
}
