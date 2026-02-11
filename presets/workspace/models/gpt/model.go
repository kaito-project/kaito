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

package gpt

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetGPT_OSS_20BModel,
		Instance: &gptOss20b,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetGPT_OSS_120BModel,
		Instance: &gptOss120b,
	})
}

const (
	// PresetGPT_OSS_20BModel is the preset name matching supported_models.yaml.
	PresetGPT_OSS_20BModel = "gpt-oss-20b"
	// PresetGPT_OSS_120BModel is the preset name matching supported_models.yaml.
	PresetGPT_OSS_120BModel = "gpt-oss-120b"
)

var (
	baseCommandPresetGPTInference = "accelerate launch"
	// GPT-OSS uses the Harmony chat format and provides its own chat template in the repo.
	// We enable allow_remote_files so Transformers can fetch it when needed.
	gptRunParams = map[string]string{
		"torch_dtype":        "auto",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	gptRunParamsVLLM = map[string]string{} // TODO: add the dtype to the gpt model
)

var gptOss20b gpt_oss_20B
var gptOss120b gpt_oss_120B

type gpt_oss_20B struct{}

func (*gpt_oss_20B) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:               metadata.MustGet(PresetGPT_OSS_20BModel),
		DiskStorageRequirement: "110Gi",
		GPUCountRequirement:    "1",
		// TotalSafeTensorFileSize: "16Gi", // per https://openai.com/index/introducing-gpt-oss/
		TotalSafeTensorFileSize: "12.9Gi", // TODO: pod failed with out of memory error on A10 with 24 GB memory.
		BytesPerToken:           34560,
		ModelTokenLimit:         131072, // max_position_embeddings from HF config
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetGPTInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    gptRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetGPT_OSS_20BModel,
				ModelRunParams: gptRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*gpt_oss_20B) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*gpt_oss_20B) SupportDistributedInference() bool {
	return false
}

func (*gpt_oss_20B) SupportTuning() bool {
	return false
}

type gpt_oss_120B struct{}

func (*gpt_oss_120B) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetGPT_OSS_120BModel),
		DiskStorageRequirement:  "250Gi", // Larger model needs more disk space
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "60.77Gi", // Single 80GB GPU requirement
		BytesPerToken:           51840,
		ModelTokenLimit:         131072, // max_position_embeddings from HF config
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetGPTInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    gptRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetGPT_OSS_120BModel,
				ModelRunParams: gptRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(45) * time.Minute, // Longer timeout for larger model
	}
}

func (*gpt_oss_120B) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*gpt_oss_120B) SupportDistributedInference() bool {
	return false
}

func (*gpt_oss_120B) SupportTuning() bool {
	return false
}
