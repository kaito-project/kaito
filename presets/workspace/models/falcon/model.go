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

package falcon

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetFalcon7BModel,
		Instance: &falconA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetFalcon7BInstructModel,
		Instance: &falconB,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetFalcon40BModel,
		Instance: &falconC,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetFalcon40BInstructModel,
		Instance: &falconD,
	})
}

const (
	PresetFalcon7BModel          = "falcon-7b"
	PresetFalcon40BModel         = "falcon-40b"
	PresetFalcon7BInstructModel  = PresetFalcon7BModel + "-instruct"
	PresetFalcon40BInstructModel = PresetFalcon40BModel + "-instruct"
)

var falconA falcon7b

type falcon7b struct{}

func (*falcon7b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetFalcon7BModel),
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "13.44Gi",
		BytesPerToken:           8192,
		ModelTokenLimit:         2048, // per requirement: uniform Falcon context window
		RuntimeParam: model.RuntimeParam{
			Transformers: metadata.TransformerInferenceParameters[PresetFalcon7BModel],
			VLLM:         metadata.VLLMInferenceParameters[PresetFalcon7BModel],
			// vllm requires the model specification to be exactly divisible by
			// the number of GPUs(tensor parallel level).
			// falcon-7b have 71 attention heads, which is a prime number.
			// So, give up tensor parallel inference.
			DisableTensorParallelism: true,
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*falcon7b) GetTuningParameters() *model.PresetParam {
	tc := metadata.TransformerTuningParameters[PresetFalcon7BModel]
	return &model.PresetParam{
		Metadata:                      metadata.MustGet(PresetFalcon7BModel),
		DiskStorageRequirement:        tc.DiskStorageRequirement,
		GPUCountRequirement:           tc.GPUCountRequirement,
		TotalSafeTensorFileSize:       tc.TotalSafeTensorFileSize,
		ModelTokenLimit:               tc.ModelTokenLimit,
		BytesPerToken:                 tc.BytesPerToken,
		TuningPerGPUMemoryRequirement: tc.TuningPerGPUMemoryRequirement,
		ReadinessTimeout:              tc.ReadinessTimeout,
		RuntimeParam: model.RuntimeParam{
			Transformers: tc.Transformers,
		},
	}
}

func (*falcon7b) SupportDistributedInference() bool {
	return false
}
func (*falcon7b) SupportTuning() bool {
	return true
}

var falconB falcon7bInst

type falcon7bInst struct{}

func (*falcon7bInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetFalcon7BInstructModel),
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "13.44Gi",
		BytesPerToken:           8192,
		ModelTokenLimit:         2048, // per requirement: uniform Falcon context window
		RuntimeParam: model.RuntimeParam{
			Transformers: metadata.TransformerInferenceParameters[PresetFalcon7BInstructModel],
			VLLM:         metadata.VLLMInferenceParameters[PresetFalcon7BInstructModel],
			// vllm requires the model specification to be exactly divisible by
			// the number of GPUs(tensor parallel level).
			// falcon-7b-instruct have 71 attention heads, which is a prime number.
			// So, give up tensor parallel inference.
			DisableTensorParallelism: true,
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

}
func (*falcon7bInst) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}
func (*falcon7bInst) SupportDistributedInference() bool {
	return false
}
func (*falcon7bInst) SupportTuning() bool {
	return false
}

var falconC falcon40b

type falcon40b struct{}

func (*falcon40b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetFalcon40BModel),
		DiskStorageRequirement:  "280Gi",
		GPUCountRequirement:     "2",
		TotalSafeTensorFileSize: "77.9Gi",
		BytesPerToken:           8192,
		ModelTokenLimit:         2048, // per requirement: uniform Falcon context window
		RuntimeParam: model.RuntimeParam{
			Transformers: metadata.TransformerInferenceParameters[PresetFalcon40BModel],
			VLLM:         metadata.VLLMInferenceParameters[PresetFalcon40BModel],
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*falcon40b) GetTuningParameters() *model.PresetParam {
	tc := metadata.TransformerTuningParameters[PresetFalcon40BModel]
	return &model.PresetParam{
		Metadata:                      metadata.MustGet(PresetFalcon40BModel),
		DiskStorageRequirement:        tc.DiskStorageRequirement,
		GPUCountRequirement:           tc.GPUCountRequirement,
		TotalSafeTensorFileSize:       tc.TotalSafeTensorFileSize,
		ModelTokenLimit:               tc.ModelTokenLimit,
		BytesPerToken:                 tc.BytesPerToken,
		TuningPerGPUMemoryRequirement: tc.TuningPerGPUMemoryRequirement,
		ReadinessTimeout:              tc.ReadinessTimeout,
		RuntimeParam: model.RuntimeParam{
			Transformers: tc.Transformers,
		},
	}
}
func (*falcon40b) SupportDistributedInference() bool {
	return false
}
func (*falcon40b) SupportTuning() bool {
	return true
}

var falconD falcon40bInst

type falcon40bInst struct{}

func (*falcon40bInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetFalcon40BInstructModel),
		DiskStorageRequirement:  "280Gi",
		GPUCountRequirement:     "2",
		TotalSafeTensorFileSize: "77.9Gi",
		BytesPerToken:           1966080,
		ModelTokenLimit:         2048, // per requirement: uniform Falcon context window
		RuntimeParam: model.RuntimeParam{
			Transformers: metadata.TransformerInferenceParameters[PresetFalcon40BInstructModel],
			VLLM:         metadata.VLLMInferenceParameters[PresetFalcon40BInstructModel],
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*falcon40bInst) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}
func (*falcon40bInst) SupportDistributedInference() bool {
	return false
}
func (*falcon40bInst) SupportTuning() bool {
	return false
}
