// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package deepseek

import (
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
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
}

var (
	PresetDeepSeekR1DistillLlama8BModel = "deepseek-r1-distill-llama-8b"
	PresetDeepSeekR1DistillQwen14BModel = "deepseek-r1-distill-qwen-14b"

	PresetDeepSeekTagMap = map[string]string{
		"DeepSeekDistillLlama8B": "0.0.1",
		"DeepSeekDistillQwen14B": "0.0.1",
	}

	baseCommandPresetDeepseekInference = "accelerate launch"
	deepseekLlama8bRunParams           = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	deepseekLlama8bRunParamsVLLM = map[string]string{
		"dtype": "float16",
	}
	deepseekQwen14bRunParams = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	deepseekQwen14bRunParamsVLLM = map[string]string{
		"dtype": "float16",
	}
)

var deepseekA llama8b

type llama8b struct{}

func (*llama8b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "DeepSeek",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "14Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				TorchRunParams:    inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefautTransformersMainFile,
				ModelRunParams:    deepseekLlama8bRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      "/workspace/vllm/weights",
				ModelRunParams: deepseekLlama8bRunParamsVLLM,
			},
			// vllm requires the model specification to be exactly divisible by
			// the number of GPUs(tensor parallel level).
			DisableTensorParallelism: true,
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
		Tag:              PresetDeepSeekTagMap["DeepSeekDistillLlama8B"],
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
		ModelFamilyName:           "DeepSeek",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "25.7Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run DeepSeek using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetDeepseekInference,
				TorchRunParams:    inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefautTransformersMainFile,
				ModelRunParams:    deepseekQwen14bRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      "/workspace/vllm/weights",
				ModelRunParams: deepseekQwen14bRunParamsVLLM,
			},
			// vllm requires the model specification to be exactly divisible by
			// the number of GPUs(tensor parallel level).
			DisableTensorParallelism: true,
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
		Tag:              PresetDeepSeekTagMap["DeepSeekDistillQwen14B"],
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
