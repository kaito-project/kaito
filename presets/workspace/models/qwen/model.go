// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package qwen

import (
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetQwen2_5Coder7BInstructModel,
		Instance: &qwen2_5coder7bInst,
	})
}

var (
	PresetQwen2_5Coder7BInstructModel = "qwen2.5-coder-7b-instruct"

	PresetTagMap = map[string]string{
		"Qwen2.5-Coder-7B-Instruct": "0.0.1",
	}

	baseCommandPresetQwenInference = "accelerate launch"
	baseCommandPresetQwenTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	qwenRunParams                  = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	qwenRunParamsVLLM = map[string]string{
		"dtype": "float16",
	}
)

var qwen2_5coder7bInst qwen2_5Coder7BInstruct

type qwen2_5Coder7BInstruct struct{}

func (*qwen2_5Coder7BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Qwen",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "100Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "24Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run qwen using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				TorchRunParams:    inference.DefaultAccelerateParams,
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
		Tag:              PresetTagMap["Qwen2.5-Coder-7B-Instruct"],
	}
}

func (*qwen2_5Coder7BInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "qwen",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "100Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "24Gi",
		PerGPUMemoryRequirement:   "24Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				//TorchRunParams:            tuning.DefaultAccelerateParams,
				//ModelRunParams:            qwenRunParams,
				BaseCommand: baseCommandPresetQwenTuning,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
		Tag:              PresetTagMap["Qwen2.5-Coder-7B-Instruct"],
	}
}

func (*qwen2_5Coder7BInstruct) SupportDistributedInference() bool {
	return false
}
func (*qwen2_5Coder7BInstruct) SupportTuning() bool {
	return true
}
