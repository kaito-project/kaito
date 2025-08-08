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
		Name:     PresetGPTOss20BModel,
		Instance: &gptOss20b,
	})
}

const (
	// PresetGPTOss20BModel is the preset name matching supported_models.yaml.
	PresetGPTOss20BModel = "gpt-oss-20b"
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
)

var gptOss20b gptOss20B

type gptOss20B struct{}

func (*gptOss20B) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetGPTOss20BModel),
		DiskStorageRequirement:    "16Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi", // per https://openai.com/index/introducing-gpt-oss/
		PerGPUMemoryRequirement:   "0Gi",  // Native vertical model parallel; no per-GPU split requirement
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetGPTInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    gptRunParams,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*gptOss20B) GetTuningParameters() *model.PresetParam {
	return nil
}

func (*gptOss20B) SupportDistributedInference() bool {
	return false
}

func (*gptOss20B) SupportTuning() bool {
	return false
}
