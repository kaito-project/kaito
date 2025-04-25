// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package phi4

import (
	"time"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

func init() {
	plugin.KaitoModelRegister.RegisterInstance(&plugin.ModelInstance{
		Name:     PresetPhi4Model,
		Instance: &phi4A,
	})
	plugin.KaitoModelRegister.RegisterInstance(&plugin.ModelInstance{
		Name:     PresetPhi4MiniInstructModel,
		Instance: &phi4MiniB,
	})
}

var (
	PresetPhi4Model             = "phi-4"
	PresetPhi4MiniInstructModel = "phi-4-mini-instruct"

	PresetPhiTagMap = map[string]string{
		"Phi4":             "0.1.0",
		"Phi4MiniInstruct": "0.1.0",
	}

	baseCommandPresetPhiInference = "accelerate launch"
	baseCommandPresetPhiTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	phiRunParams                  = map[string]string{
		"torch_dtype":       "auto",
		"pipeline":          "text-generation",
		"trust_remote_code": "",
	}
	phiRunParamsVLLM = map[string]string{
		"dtype": "float16",
	}
)

var phi4A phi4Model

type phi4Model struct{}

func (*phi4Model) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Phi4",
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "100Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "40Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "0Gi",  // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetPhiInference,
				TorchRunParams:    inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    phiRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      "phi-4",
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
		Tag:              PresetPhiTagMap["Phi4"],
	}
}

func (*phi4Model) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Phi4",
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "100Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "70Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "70Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
		Tag: PresetPhiTagMap["Phi4"],
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
		ModelFamilyName:           "Phi4",
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "8Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetPhiInference,
				TorchRunParams:    inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    phiRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      "phi-4-mini-instruct",
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
		Tag:              PresetPhiTagMap["Phi4MiniInstruct"],
	}
}

func (*phi4MiniInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Phi4",
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "72Gi", // Requires at least A100 - TODO: Revisit for more accurate metric here
		PerGPUMemoryRequirement:   "72Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
		Tag: PresetPhiTagMap["Phi4MiniInstruct"],
	}
}

func (*phi4MiniInstruct) SupportDistributedInference() bool { return false }
func (*phi4MiniInstruct) SupportTuning() bool {
	return true
}
