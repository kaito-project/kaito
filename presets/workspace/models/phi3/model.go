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

package phi3

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi3Mini4kModel,
		Instance: &phi3MiniA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi3Mini128kModel,
		Instance: &phi3MiniB,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi3Medium4kModel,
		Instance: &phi3MediumA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi3Medium128kModel,
		Instance: &phi3MediumB,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetPhi3_5MiniInstruct,
		Instance: &phi3_5MiniC,
	})
}

const (
	PresetPhi3Mini4kModel     = "phi-3-mini-4k-instruct"
	PresetPhi3Mini128kModel   = "phi-3-mini-128k-instruct"
	PresetPhi3Medium4kModel   = "phi-3-medium-4k-instruct"
	PresetPhi3Medium128kModel = "phi-3-medium-128k-instruct"
	PresetPhi3_5MiniInstruct  = "phi-3.5-mini-instruct"
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
		"dtype": "float16",
	}
)

var phi3MiniA phi3Mini4KInst

type phi3Mini4KInst struct{}

func (*phi3Mini4KInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Mini4kModel),
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "9Gi",
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
				ModelName:      PresetPhi3Mini4kModel,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*phi3Mini4KInst) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Mini4kModel),
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "72Gi",
		PerGPUMemoryRequirement:   "72Gi",
		// AccelerateParams:          inference.DefaultAccelerateParams,
		// ModelRunParams:            phiRunParams,
		ReadinessTimeout: time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}
func (*phi3Mini4KInst) SupportDistributedInference() bool { return false }
func (*phi3Mini4KInst) SupportTuning() bool {
	return true
}

var phi3MiniB phi3Mini128KInst

type phi3Mini128KInst struct{}

func (*phi3Mini128KInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Mini128kModel),
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "9Gi",
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
				ModelName:      PresetPhi3Mini128kModel,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*phi3Mini128KInst) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Mini128kModel),
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "72Gi",
		PerGPUMemoryRequirement:   "72Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}
func (*phi3Mini128KInst) SupportDistributedInference() bool { return false }
func (*phi3Mini128KInst) SupportTuning() bool {
	return true
}

var phi3_5MiniC phi3_5MiniInst

type phi3_5MiniInst struct{}

func (*phi3_5MiniInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3_5MiniInstruct),
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
				ModelName:      PresetPhi3_5MiniInstruct,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*phi3_5MiniInst) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3_5MiniInstruct),
		DiskStorageRequirement:    "70Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "72Gi",
		PerGPUMemoryRequirement:   "72Gi",
		// AccelerateParams:          inference.DefaultAccelerateParams,
		// ModelRunParams:            phiRunParams,
		ReadinessTimeout: time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}
func (*phi3_5MiniInst) SupportDistributedInference() bool { return false }
func (*phi3_5MiniInst) SupportTuning() bool {
	return true
}

var phi3MediumA Phi3Medium4kInstruct

type Phi3Medium4kInstruct struct{}

func (*Phi3Medium4kInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Medium4kModel),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "28Gi",
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
				ModelName:      PresetPhi3Medium4kModel,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*Phi3Medium4kInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Medium4kModel),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "80Gi",
		PerGPUMemoryRequirement:   "80Gi",
		// AccelerateParams:          inference.DefaultAccelerateParams,
		// ModelRunParams:            phiRunParams,
		ReadinessTimeout: time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}
func (*Phi3Medium4kInstruct) SupportDistributedInference() bool { return false }
func (*Phi3Medium4kInstruct) SupportTuning() bool {
	return true
}

var phi3MediumB Phi3Medium128kInstruct

type Phi3Medium128kInstruct struct{}

func (*Phi3Medium128kInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Medium128kModel),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "28Gi",
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
				ModelName:      PresetPhi3Medium128kModel,
				ModelRunParams: phiRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*Phi3Medium128kInstruct) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetPhi3Medium128kModel),
		DiskStorageRequirement:    "120Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "80Gi",
		PerGPUMemoryRequirement:   "80Gi",
		ReadinessTimeout:          time.Duration(30) * time.Minute,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand: baseCommandPresetPhiTuning,
			},
		},
	}
}
func (*Phi3Medium128kInstruct) SupportDistributedInference() bool { return false }
func (*Phi3Medium128kInstruct) SupportTuning() bool {
	return true
}
