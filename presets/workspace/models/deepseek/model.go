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

package deepseek

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetDeepSeekR1DistillLlama8BModel,
		Instance: &deepseekA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetDeepSeekR1DistillQwen14BModel,
		Instance: &deepseekB,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetDeepSeekR1Model,
		Instance: &deepseekC,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetDeepSeekV3Model,
		Instance: &deepseekD,
	})
}

const (
	PresetDeepSeekR1DistillLlama8BModel = "deepseek-r1-distill-llama-8b"
	PresetDeepSeekR1DistillQwen14BModel = "deepseek-r1-distill-qwen-14b"
	PresetDeepSeekR1Model               = "deepseek-r1-0528"
	PresetDeepSeekV3Model               = "deepseek-v3-0324"
)

var (
	baseCommandPresetDeepseekInference = "accelerate launch"
	deepseekLlama8bRunParams           = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	deepseekLlama8bRunParamsVLLM = map[string]string{
		"dtype":            "float16",
		"enable-reasoning": "",
		"reasoning-parser": "deepseek_r1",
	}
	deepseekQwen14bRunParams = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	deepseekQwen14bRunParamsVLLM = map[string]string{
		"dtype":            "float16",
		"enable-reasoning": "",
		"reasoning-parser": "deepseek_r1",
	}
	deepseekR1RunParams = map[string]string{
		"torch_dtype":        "bfloat16",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	deepseekR1RunParamsVLLM = map[string]string{
		"enable-reasoning":        "",
		"reasoning-parser":        "deepseek_r1",
		"chat-template":           "/workspace/chat_templates/tool-chat-deepseekr1.jinja",
		"tool-call-parser":        "deepseek_v3",
		"enable-auto-tool-choice": "",
	}
	deepseekV3RunParams = map[string]string{
		"torch_dtype":        "bfloat16",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	deepseekV3RunParamsVLLM = map[string]string{
		"chat-template":           "/workspace/chat_templates/tool-chat-deepseekv3.jinja",
		"tool-call-parser":        "deepseek_v3",
		"enable-auto-tool-choice": "",
	}
)

var deepseekA llama8b

type llama8b struct{}

func (*llama8b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetDeepSeekR1DistillLlama8BModel),
		DiskStorageRequirement:    "90Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16.5Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    deepseekLlama8bRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetDeepSeekR1DistillLlama8BModel,
				ModelRunParams: deepseekLlama8bRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*llama8b) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*llama8b) SupportDistributedInference() bool {
	return false
}
func (*llama8b) SupportTuning() bool {
	return false
}

var deepseekB qwen14b

type qwen14b struct{}

func (*qwen14b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetDeepSeekR1DistillQwen14BModel),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "25.7Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    deepseekQwen14bRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetDeepSeekR1DistillQwen14BModel,
				ModelRunParams: deepseekQwen14bRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*qwen14b) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*qwen14b) SupportDistributedInference() bool {
	return false
}
func (*qwen14b) SupportTuning() bool {
	return false
}

var deepseekC deepseekR1

type deepseekR1 struct{}

func (*deepseekR1) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetDeepSeekR1Model),
		DiskStorageRequirement:    "800Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "960Gi", // at least 8 H100
		PerGPUMemoryRequirement:   "0Gi",   // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    deepseekR1RunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:          inference.DefaultVLLMCommand,
				ModelName:            PresetDeepSeekR1Model,
				ModelRunParams:       deepseekR1RunParamsVLLM,
				RayLeaderBaseCommand: inference.DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: inference.DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*deepseekR1) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*deepseekR1) SupportDistributedInference() bool {
	return true
}
func (*deepseekR1) SupportTuning() bool {
	return false
}

var deepseekD deepseekV3

type deepseekV3 struct{}

func (*deepseekV3) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetDeepSeekR1Model),
		DiskStorageRequirement:    "800Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "960Gi", // at least 8 H100
		PerGPUMemoryRequirement:   "0Gi",   // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    deepseekV3RunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:          inference.DefaultVLLMCommand,
				ModelName:            PresetDeepSeekR1Model,
				ModelRunParams:       deepseekV3RunParamsVLLM,
				RayLeaderBaseCommand: inference.DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: inference.DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*deepseekV3) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*deepseekV3) SupportDistributedInference() bool {
	return true
}
func (*deepseekV3) SupportTuning() bool {
	return false
}
