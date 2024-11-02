// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package phi2

import (
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/inference"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi2Model,
		Instance: &phiA,
	})
}

var (
	PresetPhi2Model = "phi-2"

	PresetPhiTagMap = map[string]string{
		"Phi2": "0.0.5",
	}

	baseCommandPresetPhiInference = "accelerate launch"
	baseCommandPresetPhiTuning    = "python3 metrics_server.py & accelerate launch"
	phiRunParams                  = map[string]string{
		"torch_dtype": "float16",
		"pipeline":    "text-generation",
	}
)

var phiA phi2

type phi2 struct{}

func (*phi2) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Phi",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "12Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Phi using native vertical model parallel, no per GPU memory requirement.
		TorchRunParams:            inference.DefaultAccelerateParams,
		ModelRunParams:            phiRunParams,
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		BaseCommand:               baseCommandPresetPhiInference,
		Tag:                       PresetPhiTagMap["Phi2"],
	}
}
func (*phi2) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		ModelFamilyName:           "Phi",
		ImageAccessMode:           string(kaitov1alpha1.ModelImageAccessModePublic),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "16Gi",
		// TorchRunParams:            inference.DefaultAccelerateParams,
		// ModelRunParams:            phiRunParams,
		ReadinessTimeout: time.Duration(30) * time.Minute,
		BaseCommand:      baseCommandPresetPhiTuning,
		Tag:              PresetPhiTagMap["Phi2"],
	}
}
func (*phi2) SupportDistributedInference() bool {
	return false
}
func (*phi2) SupportTuning() bool {
	return true
}
