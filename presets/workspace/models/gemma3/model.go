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

package gemma3

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	PresetGemma3_4BInstructModel  = "gemma-3-4b-instruct"
	PresetGemma3_27BInstructModel = "gemma-3-27b-instruct"
)

var (
	baseCommandPresetGemma3Inference = "accelerate launch"
	gemma3RunParams                  = map[string]string{
		"torch_dtype":        "auto",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	gemma3RunParamsVLLM = map[string]string{
		"dtype": "bfloat16",
	}
)

var gemma3_4bInst gemma3_4BInstruct
var gemma3_27bInst gemma3_27BInstruct

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetGemma3_4BInstructModel,
		Instance: &gemma3_4bInst,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetGemma3_27BInstructModel,
		Instance: &gemma3_27bInst,
	})
}

type gemma3_4BInstruct struct{}

func (*gemma3_4BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetGemma3_4BInstructModel),
		DiskStorageRequirement:  "60Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "8.6Gi",
		BytesPerToken:           348160,
		ModelTokenLimit:         131072,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetGemma3Inference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    gemma3RunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetGemma3_4BInstructModel,
				ModelRunParams: gemma3RunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*gemma3_4BInstruct) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*gemma3_4BInstruct) SupportDistributedInference() bool {
	return false
}

func (*gemma3_4BInstruct) SupportTuning() bool {
	return false
}

type gemma3_27BInstruct struct{}

func (*gemma3_27BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetGemma3_27BInstructModel),
		DiskStorageRequirement:  "200Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "54.8Gi",
		BytesPerToken:           666624,
		ModelTokenLimit:         131072,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetGemma3Inference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    gemma3RunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetGemma3_27BInstructModel,
				ModelRunParams: gemma3RunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(45) * time.Minute,
	}
}

func (*gemma3_27BInstruct) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*gemma3_27BInstruct) SupportDistributedInference() bool {
	return false
}

func (*gemma3_27BInstruct) SupportTuning() bool {
	return false
}
