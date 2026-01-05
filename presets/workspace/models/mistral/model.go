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

package mistral

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMistral7BModel,
		Instance: &mistralA,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMistral7BInstructModel,
		Instance: &mistralB,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMinistral33BInstructModel,
		Instance: &ministral3_3bInst,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMinistral38BInstructModel,
		Instance: &ministral3_8bInst,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMinistral314BInstructModel,
		Instance: &ministral3_14bInst,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     PresetMistralLarge3675BInstructModel,
		Instance: &mistralLarge3_675bInst,
	})
}

const (
	PresetMistral7BModel         = "mistral-7b"
	PresetMistral7BInstructModel = PresetMistral7BModel + "-instruct"

	PresetMinistral33BInstructModel      = "ministral-3-3b-instruct"
	PresetMinistral38BInstructModel      = "ministral-3-8b-instruct"
	PresetMinistral314BInstructModel     = "ministral-3-14b-instruct"
	PresetMistralLarge3675BInstructModel = "mistral-large-3-675b-instruct"
)

var (
	baseCommandPresetMistralInference = "accelerate launch"
	baseCommandPresetMistralTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	mistralRunParams                  = map[string]string{
		"torch_dtype":   "bfloat16",
		"pipeline":      "text-generation",
		"chat_template": "/workspace/chat_templates/mistral-instruct.jinja",
	}
	mistralRunParamsVLLM = map[string]string{
		"dtype":                   "float16",
		"tool-call-parser":        "mistral",
		"enable-auto-tool-choice": "",
	}
	mistral3RunParamsVLLM = map[string]string{
		"dtype":                   "float16",
		"tool-call-parser":        "mistral",
		"tokenizer_mode":          "mistral",
		"config_format":           "mistral",
		"load_format":             "mistral",
		"enable-auto-tool-choice": "",
	}
)

var mistralA mistral7b

type mistral7b struct{}

func (*mistral7b) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMistral7BModel),
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "13.49Gi",
		BytesPerToken:           131072,
		ModelTokenLimit:         32768, // max_position_embeddings from HF config (v0.1/0.2)
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetMistral7BModel,
				ModelRunParams: mistralRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

}
func (*mistral7b) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMistral7BModel),
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "16Gi",
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				// AccelerateParams: tuning.DefaultAccelerateParams,
				// ModelRunParams:   mistralRunParams,
				BaseCommand: baseCommandPresetMistralTuning,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*mistral7b) SupportDistributedInference() bool {
	return false
}
func (*mistral7b) SupportTuning() bool {
	return true
}

var mistralB mistral7bInst

type mistral7bInst struct{}

func (*mistral7bInst) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMistral7BInstructModel),
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "13.49Gi",
		BytesPerToken:           131072,
		ModelTokenLimit:         32768, // max_position_embeddings from HF config (v0.2)
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetMistral7BInstructModel,
				ModelRunParams: mistralRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}

}
func (*mistral7bInst) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}
func (*mistral7bInst) SupportDistributedInference() bool {
	return false
}
func (*mistral7bInst) SupportTuning() bool {
	return false
}

var ministral3_3bInst ministral3_3bInstruct
var ministral3_8bInst ministral3_8bInstruct
var ministral3_14bInst ministral3_14bInstruct
var mistralLarge3_675bInst mistralLarge3_675bInstruct

type ministral3_3bInstruct struct{}

func (*ministral3_3bInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMinistral33BInstructModel),
		DiskStorageRequirement:  "70Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "8.70Gi",
		BytesPerToken:           106496,
		ModelTokenLimit:         262144,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetMinistral33BInstructModel,
				ModelRunParams: mistral3RunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*ministral3_3bInstruct) GetTuningParameters() *model.PresetParam { return nil }
func (*ministral3_3bInstruct) SupportDistributedInference() bool       { return false }
func (*ministral3_3bInstruct) SupportTuning() bool                     { return false }

type ministral3_8bInstruct struct{}

func (*ministral3_8bInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMinistral38BInstructModel),
		DiskStorageRequirement:  "100Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "19.41Gi",
		BytesPerToken:           139264,
		ModelTokenLimit:         262144,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetMinistral38BInstructModel,
				ModelRunParams: mistral3RunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*ministral3_8bInstruct) GetTuningParameters() *model.PresetParam { return nil }
func (*ministral3_8bInstruct) SupportDistributedInference() bool       { return false }
func (*ministral3_8bInstruct) SupportTuning() bool                     { return false }

type ministral3_14bInstruct struct{}

func (*ministral3_14bInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMinistral314BInstructModel),
		DiskStorageRequirement:  "100Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "29.29Gi",
		BytesPerToken:           163840,
		ModelTokenLimit:         262144,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    metadata.DefaultVLLMCommand,
				ModelName:      PresetMinistral314BInstructModel,
				ModelRunParams: mistral3RunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*ministral3_14bInstruct) GetTuningParameters() *model.PresetParam { return nil }
func (*ministral3_14bInstruct) SupportDistributedInference() bool       { return false }
func (*ministral3_14bInstruct) SupportTuning() bool                     { return false }

type mistralLarge3_675bInstruct struct{}

func (*mistralLarge3_675bInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                metadata.MustGet(PresetMistralLarge3675BInstructModel),
		DiskStorageRequirement:  "800Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "634.70Gi",
		// According to Multi-Head Latent Attention (MLA).
		// BytesPerToken = 2 * (kv_lora_rank + qk_rope_head_dim) * num_hidden_layers
		BytesPerToken:   70272,
		ModelTokenLimit: 262144,
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				AccelerateParams:  inference.DefaultAccelerateParams,
				ModelRunParams:    mistralRunParams,
				BaseCommand:       baseCommandPresetMistralInference,
				InferenceMainFile: inference.DefaultTransformersMainFile,
			},
			VLLM: model.VLLMParam{
				BaseCommand:          metadata.DefaultVLLMCommand,
				ModelName:            PresetMistralLarge3675BInstructModel,
				ModelRunParams:       mistral3RunParamsVLLM,
				RayLeaderBaseCommand: metadata.DefaultVLLMRayLeaderBaseCommand,
				RayWorkerBaseCommand: metadata.DefaultVLLMRayWorkerBaseCommand,
			},
		},
		ReadinessTimeout: time.Duration(60) * time.Minute,
	}
}
func (*mistralLarge3_675bInstruct) GetTuningParameters() *model.PresetParam { return nil }
func (*mistralLarge3_675bInstruct) SupportDistributedInference() bool       { return true }
func (*mistralLarge3_675bInstruct) SupportTuning() bool                     { return false }
