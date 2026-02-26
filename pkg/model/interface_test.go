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

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPresetParamDeepCopy(t *testing.T) {
	original := &PresetParam{
		Metadata:            Metadata{Name: "test-model"},
		GPUCountRequirement: "2",
		TuningPerGPUMemoryRequirement: map[string]int{
			"lora": 16000,
		},
		RuntimeParam: RuntimeParam{
			Transformers: HuggingfaceTransformersParam{
				BaseCommand:      "accelerate launch",
				AccelerateParams: map[string]string{"num_processes": "2"},
				ModelRunParams:   map[string]string{"model": "test"},
			},
			VLLM: VLLMParam{
				BaseCommand:    "vllm serve",
				ModelRunParams: map[string]string{"tensor-parallel-size": "2"},
			},
		},
	}

	copied := original.DeepCopy()
	require.NotNil(t, copied)
	assert.Equal(t, original.Metadata.Name, copied.Metadata.Name)
	assert.Equal(t, original.GPUCountRequirement, copied.GPUCountRequirement)
	assert.Equal(t, original.TuningPerGPUMemoryRequirement, copied.TuningPerGPUMemoryRequirement)

	// Mutations on the copy must not affect the original.
	copied.TuningPerGPUMemoryRequirement["lora"] = 0
	assert.Equal(t, 16000, original.TuningPerGPUMemoryRequirement["lora"])

	copied.RuntimeParam.Transformers.AccelerateParams["num_processes"] = "8"
	assert.Equal(t, "2", original.RuntimeParam.Transformers.AccelerateParams["num_processes"])
}

func TestRuntimeParamDeepCopy(t *testing.T) {
	rp := RuntimeParam{
		Transformers: HuggingfaceTransformersParam{
			AccelerateParams: map[string]string{"a": "1"},
			ModelRunParams:   map[string]string{"b": "2"},
		},
		VLLM: VLLMParam{
			RayLeaderParams: map[string]string{"c": "3"},
			RayWorkerParams: map[string]string{"d": "4"},
			ModelRunParams:  map[string]string{"e": "5"},
		},
	}

	copied := rp.DeepCopy()
	copied.Transformers.AccelerateParams["a"] = "99"
	assert.Equal(t, "1", rp.Transformers.AccelerateParams["a"])

	copied.VLLM.RayLeaderParams["c"] = "99"
	assert.Equal(t, "3", rp.VLLM.RayLeaderParams["c"])
}

func TestHuggingfaceTransformersParamDeepCopy(t *testing.T) {
	h := HuggingfaceTransformersParam{
		BaseCommand:      "accelerate",
		AccelerateParams: map[string]string{"k": "v"},
		ModelRunParams:   map[string]string{"m": "n"},
	}
	copied := h.DeepCopy()
	copied.AccelerateParams["k"] = "changed"
	assert.Equal(t, "v", h.AccelerateParams["k"])
}

func TestVLLMParamDeepCopy(t *testing.T) {
	v := VLLMParam{
		BaseCommand:     "vllm serve",
		RayLeaderParams: map[string]string{"leader": "1"},
		RayWorkerParams: map[string]string{"worker": "2"},
		ModelRunParams:  map[string]string{"model": "3"},
	}
	copied := v.DeepCopy()
	copied.RayLeaderParams["leader"] = "99"
	assert.Equal(t, "1", v.RayLeaderParams["leader"])

	copied.RayWorkerParams["worker"] = "99"
	assert.Equal(t, "2", v.RayWorkerParams["worker"])
}

func TestGetInferenceCommandHuggingface(t *testing.T) {
	p := &PresetParam{
		RuntimeParam: RuntimeParam{
			Transformers: HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				AccelerateParams:  map[string]string{},
				InferenceMainFile: "/workspace/inference.py",
				ModelRunParams:    map[string]string{"num-gpus": "1"},
			},
		},
	}
	rc := RuntimeContext{RuntimeName: RuntimeNameHuggingfaceTransformers}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Equal(t, "/bin/sh", cmd[0])
	assert.Equal(t, "-c", cmd[1])
	assert.Contains(t, cmd[2], "accelerate launch")
	assert.Contains(t, cmd[2], "/workspace/inference.py")
}

func TestGetInferenceCommandHuggingfaceWithModelName(t *testing.T) {
	p := &PresetParam{
		RuntimeParam: RuntimeParam{
			Transformers: HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				AccelerateParams:  map[string]string{},
				InferenceMainFile: "/workspace/inference.py",
				ModelRunParams:    map[string]string{},
				ModelName:         "test-served-model",
			},
		},
	}
	rc := RuntimeContext{RuntimeName: RuntimeNameHuggingfaceTransformers}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "test-served-model")
}

func TestGetInferenceCommandVLLMSingleNode(t *testing.T) {
	p := &PresetParam{
		RuntimeParam: RuntimeParam{
			VLLM: VLLMParam{
				BaseCommand:    "vllm serve",
				ModelRunParams: map[string]string{},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName:          RuntimeNameVLLM,
		SKUNumGPUs:           2,
		NumNodes:             1,
		DistributedInference: false,
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "vllm serve")
	assert.Contains(t, cmd[2], "tensor-parallel-size=2")
}

func TestGetInferenceCommandVLLMMultiNode(t *testing.T) {
	p := &PresetParam{
		RuntimeParam: RuntimeParam{
			VLLM: VLLMParam{
				BaseCommand:          "vllm serve",
				ModelRunParams:       map[string]string{},
				RayLeaderBaseCommand: "ray start --head",
				RayLeaderParams:      map[string]string{},
				RayWorkerBaseCommand: "ray start",
				RayWorkerParams:      map[string]string{},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName:          RuntimeNameVLLM,
		SKUNumGPUs:           4,
		NumNodes:             2,
		DistributedInference: true,
		WorkspaceMetadata:    metav1.ObjectMeta{Name: "ws", Namespace: "default"},
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	// Multi-node path wraps in if/else on POD_INDEX
	assert.Contains(t, cmd[2], "POD_INDEX")
}

func TestGetInferenceCommandUnknownRuntime(t *testing.T) {
	p := &PresetParam{}
	rc := RuntimeContext{RuntimeName: "unknown-runtime"}
	cmd := p.GetInferenceCommand(rc)
	assert.Nil(t, cmd)
}

func TestPresetParamValidate(t *testing.T) {
	t.Run("vllm with lora disallowed and adapters enabled", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{ModelName: "llama2", DisallowLoRA: true},
			},
		}
		err := p.Validate(RuntimeContext{
			RuntimeName:                  RuntimeNameVLLM,
			RuntimeContextExtraArguments: RuntimeContextExtraArguments{AdaptersEnabled: true},
		})
		assert.Error(t, err)
	})

	t.Run("vllm with adapter strength enabled", func(t *testing.T) {
		p := &PresetParam{}
		err := p.Validate(RuntimeContext{
			RuntimeName:                  RuntimeNameVLLM,
			RuntimeContextExtraArguments: RuntimeContextExtraArguments{AdapterStrengthEnabled: true},
		})
		assert.Error(t, err)
	})

	t.Run("vllm lora allowed with adapters enabled", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{VLLM: VLLMParam{DisallowLoRA: false}},
		}
		err := p.Validate(RuntimeContext{
			RuntimeName:                  RuntimeNameVLLM,
			RuntimeContextExtraArguments: RuntimeContextExtraArguments{AdaptersEnabled: true},
		})
		assert.NoError(t, err)
	})

	t.Run("transformers runtime always valid", func(t *testing.T) {
		p := &PresetParam{}
		err := p.Validate(RuntimeContext{RuntimeName: RuntimeNameHuggingfaceTransformers})
		assert.NoError(t, err)
	})
}

func TestGetTuningCommand(t *testing.T) {
	p := &PresetParam{
		RuntimeParam: RuntimeParam{
			Transformers: HuggingfaceTransformersParam{
				BaseCommand:      "accelerate launch",
				AccelerateParams: map[string]string{},
				ModelRunParams:   map[string]string{"epochs": "3"},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName: RuntimeNameHuggingfaceTransformers,
		SKUNumGPUs:  4,
	}
	cmd := p.GetTuningCommand(rc)
	require.Len(t, cmd, 3)
	assert.Equal(t, "/bin/sh", cmd[0])
	assert.Contains(t, cmd[2], "accelerate launch")
	assert.Contains(t, cmd[2], DefaultTuningMainFile)
	assert.Contains(t, cmd[2], "num_processes=4")
}
