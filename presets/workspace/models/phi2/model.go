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

package phi2

import (
	"time"

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
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "12Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Phi using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    phiRunParams,
				BaseCommand:       baseCommandPresetPhiInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    "VLLM_USE_V1=0 python3 /workspace/vllm/inference_api.py",
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
		DiskStorageRequirement:    "80Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "16Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				// AccelerateParams: inference.DefaultAccelerateParams,
				// ModelRunParams:   phiRunParams,
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
