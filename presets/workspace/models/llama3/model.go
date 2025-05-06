// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
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
}

const (
	PresetLlama3_1_8BInstructModel = "llama-3.1-8b-instruct"
)

var (
	baseCommandPresetLlamaInference = "accelerate launch"
	// baseCommandPresetLlamaTuning    = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"
	llamaRunParams = map[string]string{
		"torch_dtype":   "bfloat16",
		"pipeline":      "text-generation",
		"chat_template": "/workspace/chat_templates/llama-instruct.jinja",
	}
	llamaRunParamsVLLM = map[string]string{
		"dtype":         "float16",
		"chat-template": "/workspace/chat_templates/llama-instruct.jinja",
	}
)

var llama3_1_8b_instructA llama3_1_8BInstruct

type llama3_1_8BInstruct struct{}

// TODO: Get a more exact GPU Memory requirement (currently we know it must be >16Gi)
// https://huggingface.co/meta-llama/Llama-3.1-8B-Instruct/discussions/77
func (*llama3_1_8BInstruct) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(PresetLlama3_1_8BInstructModel),
		DiskStorageRequirement:    "50Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "22Gi",
		PerGPUMemoryRequirement:   "0Gi", // We run Llama using native vertical model parallel, no per GPU memory requirement.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       baseCommandPresetLlamaInference,
				TorchRunParams:    inference.DefaultAccelerateParams,
				InferenceMainFile: inference.DefaultTransformersMainFile,
				ModelRunParams:    llamaRunParams,
			},
			VLLM: model.VLLMParam{
				BaseCommand:    inference.DefaultVLLMCommand,
				ModelName:      PresetLlama3_1_8BInstructModel,
				ModelRunParams: llamaRunParamsVLLM,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*llama3_1_8BInstruct) GetTuningParameters() *model.PresetParam {
	return nil // It is not recommended/ideal to further fine-tune instruct models - Already been fine-tuned
}

func (*llama3_1_8BInstruct) SupportDistributedInference() bool {
	return false
}

func (*llama3_1_8BInstruct) SupportTuning() bool {
	return false
}
