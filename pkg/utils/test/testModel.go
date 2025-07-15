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

package test

import (
	"time"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

type baseTestModel struct{}

var emptyParams = map[string]string{}

func (*baseTestModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Name: "test-model",
			Tag:  "1.0.0",
		},
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "8Gi",
		DiskStorageRequirement:    "100Gi",
		RuntimeParam: model.RuntimeParam{
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelName:      "mymodel",
				ModelRunParams: emptyParams,
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
				AccelerateParams:  emptyParams,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*baseTestModel) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement: "1",
		ReadinessTimeout:    time.Duration(30) * time.Minute,
	}
}
func (*baseTestModel) SupportDistributedInference() bool {
	return true
}
func (*baseTestModel) SupportTuning() bool {
	return true
}

type testModel struct {
	baseTestModel
}

func (*testModel) SupportDistributedInference() bool {
	return false
}

type testDistributedModel struct {
	baseTestModel
}

func (*testDistributedModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Name: "test-distributed-model",
			Tag:  "1.0.0",
		},
		GPUCountRequirement:       "2",
		DiskStorageRequirement:    "100Gi",
		TotalGPUMemoryRequirement: "64Gi",
		RuntimeParam: model.RuntimeParam{
			DisableTensorParallelism: true,
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelRunParams: emptyParams,
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*testDistributedModel) SupportDistributedInference() bool {
	return true
}

type testNoTensorParallelModel struct {
	baseTestModel
}

func (*testNoTensorParallelModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Name: "test-no-tensor-parallel-model",
			Tag:  "1.0.0",
		},
		GPUCountRequirement:       "1",
		DiskStorageRequirement:    "100Gi",
		TotalGPUMemoryRequirement: "8Gi",
		RuntimeParam: model.RuntimeParam{
			DisableTensorParallelism: true,
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelRunParams: emptyParams,
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*testNoTensorParallelModel) SupportDistributedInference() bool {
	return false
}

type testNoLoraSupportModel struct {
	baseTestModel
}

type testModelDownload struct {
	baseTestModel
}

func (*testModelDownload) SupportDistributedInference() bool {
	return true
}

func (*testModelDownload) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Name:              "test-model-download",
			Tag:               "1.0.0",
			Version:           "https://huggingface.co/test-repo/test-model/commit/test-revision",
			DownloadAtRuntime: true,
		},
		GPUCountRequirement:       "1",
		DiskStorageRequirement:    "100Gi",
		TotalGPUMemoryRequirement: "64Gi",
		RuntimeParam: model.RuntimeParam{
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelRunParams: emptyParams,
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
				AccelerateParams:  emptyParams,
				ModelRunParams:    emptyParams,
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}

func (*testNoLoraSupportModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Name: "test-no-lora-support-model",
			Tag:  "1.0.0",
		},
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "8Gi",
		RuntimeParam: model.RuntimeParam{
			DisableTensorParallelism: true,
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelRunParams: emptyParams,
				DisallowLoRA:   true,
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
			},
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	}
}
func (*testNoLoraSupportModel) SupportDistributedInference() bool {
	return false
}

func RegisterTestModel() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-model",
		Instance: &testModel{},
	})

	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-distributed-model",
		Instance: &testDistributedModel{},
	})

	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-no-tensor-parallel-model",
		Instance: &testNoTensorParallelModel{},
	})

	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-no-lora-support-model",
		Instance: &testNoLoraSupportModel{},
	})

	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-model-download",
		Instance: &testModelDownload{},
	})
}
