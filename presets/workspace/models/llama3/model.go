// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package llama2chat

import (
	"time"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func init() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "llama3.1-8b-instruct",
		Instance: &llama3_1_8b_instructA,
	})
}

const (
	Llama3_1_8BChat = "llama3.1-8b-instruct"
)

var (
	baseCommandPresetLlama = "cd /workspace/llama/llama-2 && torchrun"
	llamaRunParams         = map[string]string{
		"max_seq_len":    "512",
		"max_batch_size": "8",
	}
)

var llam3_1chatA Llama3_1_8BChat

type Llama3_1_8BChat struct{}

func (*llam3_1chatA) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                  metadata.MustGet(Llama7BChat),
		ImageAccessMode:           string(kaitov1beta1.ModelImageAccessModePrivate),
		DiskStorageRequirement:    "34Gi",
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "14Gi", // We run llama2 using tensor parallelism, the memory of each GPU needs to be bigger than the tensor shard size.
		RuntimeParam: model.RuntimeParam{
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:        baseCommandPresetLlama,
				TorchRunParams:     inference.DefaultTorchRunParams,
				TorchRunRdzvParams: inference.DefaultTorchRunRdzvParams,
				InferenceMainFile:  "inference_api.py",
				ModelRunParams:     llamaRunParams,
			},
		},
		ReadinessTimeout: time.Duration(10) * time.Minute,
		WorldSize:        1,
		// Tag:  llama has private image access mode. The image tag is determined by the user.
	}
}
func (*llama2Chat7b) GetTuningParameters() *model.PresetParam {
	return nil // Currently doesn't support fine-tuning
}
func (*llama2Chat7b) SupportDistributedInference() bool {
	return false
}
func (*llama2Chat7b) SupportTuning() bool {
	return false
}

