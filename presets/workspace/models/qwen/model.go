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

package qwen

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetQwen2_5Coder7BInstructModel,
		Instance: &qwen2_5coder7bInst,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetQwen2_5Coder32BInstructModel,
		Instance: &qwen2_5coder32bInst,
	})
}

const (
	PresetQwen2_5Coder7BInstructModel  = "qwen2.5-coder-7b-instruct"
	PresetQwen2_5Coder32BInstructModel = "qwen2.5-coder-32b-instruct"
)

var (
	baseCommandPresetQwenInference = "accelerate launch"
	baseCommandPresetQwenTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	qwenRunParams                  = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	qwenRunParamsVLLM = map[string]string{
		"dtype":                   "float16",
		"chat-template":           "/workspace/chat_templates/tool-chat-hermes.jinja",
		"tool-call-parser":        "hermes",
		"enable-auto-tool-choice": "",
	}
)

var qwen2_5coder7bInst qwen2_5Coder7BInstruct

type qwen2_5Coder7BInstruct struct{}

func (*qwen2_5Coder7BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder7BInstructModel),
		DiskStorageRequirement:    "110Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "24Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run qwen using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    qwenRunParams,
				BaseCommand:       baseCommandPresetQwenInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetQwen2_5Coder7BInstructModel,
				ModelRunParams: qwenRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*qwen2_5Coder7BInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder7BInstructModel),
		DiskStorageRequirement:    "110Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "24Gi",
		PerGPUMemoryRequirement:   "24Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				// AccelerateParams: tuning.DefaultAccelerateParams,
				// ModelRunParams:   qwenRunParams,
				BaseCommand: baseCommandPresetQwenTuning,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*qwen2_5Coder7BInstruct) SupportDistributedInference() bool {
	return false
}
func (*qwen2_5Coder7BInstruct) SupportTuning() bool {
	return true
}

var qwen2_5coder32bInst qwen2_5Coder32BInstruct

type qwen2_5Coder32BInstruct struct{}

func (*qwen2_5Coder32BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder32BInstructModel),
		DiskStorageRequirement:    "230Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "70Gi", // Requires at least A100 - TODO: Revisit for more accurate metric
		PerGPUMemoryRequirement:   "0Gi",  // We run qwen using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    qwenRunParams,
				BaseCommand:       baseCommandPresetQwenInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetQwen2_5Coder32BInstructModel,
				ModelRunParams: qwenRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*qwen2_5Coder32BInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder32BInstructModel),
		DiskStorageRequirement:    "230Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "140Gi", // Requires at least 2 A100 - TODO: Revisit for more accurate metric
		PerGPUMemoryRequirement:   "70Gi",  // TODO: Revisit for more accurate metric
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetQwenTuning,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*qwen2_5Coder32BInstruct) SupportDistributedInference() bool {
	return false
}
func (*qwen2_5Coder32BInstruct) SupportTuning() bool {
	return true
}
