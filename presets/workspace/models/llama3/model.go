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

package llama3

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetLlama3_1_8BInstructModel,
		Instance: &llama3_1_8b_instructA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetLlama3_3_70BInstructModel,
		Instance: &llama3_3_70b_instruct,
	})
}

const (
	PresetLlama3_1_8BInstructModel  = "llama-3.1-8b-instruct"
	PresetLlama3_3_70BInstructModel = "llama-3.3-70b-instruct"
)

var (
	baseCommandPresetLlamaInference = "accelerate launch"
	// baseCommandPresetLlamaTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	llamaRunParams = map[string]string{
		"torch_dtype":        "bfloat16",
		"pipeline":           "text-generation",
		"chat_template":      "/workspace/chat_templates/llama-3-instruct.jinja",
		"allow_remote_files": "",
	}
	llamaRunParamsVLLM = map[string]string{
		"dtype":                   "float16",
		"chat-template":           "/workspace/chat_templates/tool-chat-llama3.1-json.jinja",
		"tool-call-parser":        "llama3_json",
		"enable-auto-tool-choice": "",
	}
)

var llama3_1_8b_instructA llama3_1_8BInstruct

type llama3_1_8BInstruct struct{}

func (*llama3_1_8BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetLlama3_1_8BInstructModel),
		DiskStorageRequirement:    "110Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "22Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Llama using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetLlamaInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    llamaRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:          inference.DefaultVLLMCommand,
				ModelName:            PresetLlama3_1_8BInstructModel,
				ModelRunParams:       llamaRunParamsVLLM,
				RayLeaderBaseCommand: inference.DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: inference.DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*llama3_1_8BInstruct) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}

func (*llama3_1_8BInstruct) SupportDistributedInference() bool {
	return true
}

func (*llama3_1_8BInstruct) SupportTuning() bool {
	return false
}

var llama3_3_70b_instruct llama3_3_70Binstruct

type llama3_3_70Binstruct struct{}

// TODO: Get a more exact GPU Memory requirement (currently we know it must be >16Gi)
// https://huggingface.co/meta-llama/Llama-3.1-8B-Instruct/discussions/77
func (*llama3_3_70Binstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetLlama3_3_70BInstructModel),
		DiskStorageRequirement:    "220Gi",
		GPUCountRequirement:       "4",
		TotalGPUMemoryRequirement: "320Gi",
		PerGPUMemoryRequirement:   "80Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetLlamaInference,
				AccelerateParams:  inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    llamaRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:          inference.DefaultVLLMCommand,
				ModelName:            PresetLlama3_3_70BInstructModel,
				ModelRunParams:       llamaRunParamsVLLM,
				RayLeaderBaseCommand: inference.DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: inference.DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*llama3_3_70Binstruct) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}

func (*llama3_3_70Binstruct) SupportDistributedInference() bool {
	return true
}

func (*llama3_3_70Binstruct) SupportTuning() bool {
	return false
}
