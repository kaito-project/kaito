// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package qwen

import (
	"time"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
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
		"dtype": "float16",
	}
)

var qwen2_5coder7bInst qwen2_5Coder7BInstruct

type qwen2_5Coder7BInstruct struct{}

func (*qwen2_5Coder7BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder7BInstructModel),
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
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
	}
}

func (*qwen2_5Coder7BInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetQwen2_5Coder7BInstructModel),
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
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
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "70Gi", // Requires at least A100 - TODO: Revisit for more accurate metric
		PerGPUMemoryRequirement:   "0Gi",  // We run qwen using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				TorchRunParams:    inference.DefaultAccelerateParams,
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
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "120Gi",
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
