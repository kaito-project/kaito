// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package phi2

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
		Name:     PresetPhi2Model,
		Instance: &phiA,
	})
}

const (
	PresetPhi2Model = "phi-2"
)

var (
	baseCommandPresetPhiInference = "accelerate launch"
	baseCommandPresetPhiTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	phiRunParams                  = map[string]string{
		"torch_dtype": "float16",
		"pipeline":    "text-generation",
	}
	phiRunParamsVLLM = map[string]string{
		"dtype": "float16",
	}
)

var phiA phi2

type phi2 struct{}

func (*phi2) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi2Model),
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "12Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				TorchRunParams:    inference.DefaultAccelerateParams,
				ModelRunParams:    phiRunParams,
				BaseCommand:       baseCommandPresetPhiInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetPhi2Model,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*phi2) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi2Model),
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "16Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				// TorchRunParams:            inference.DefaultAccelerateParams,
				// ModelRunParams:            phiRunParams,
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*phi2) SupportDistributedInference() bool {
	return false
}
func (*phi2) SupportTuning() bool {
	return true
}
