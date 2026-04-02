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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/sku"
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

func TestGetInferenceCommandVLLMPerformanceMode(t *testing.T) {
	newPreset := func() *PresetParam {
		return &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{
					BaseCommand:    "vllm serve",
					ModelRunParams: map[string]string{},
				},
			},
		}
	}
	baseRC := RuntimeContext{
		RuntimeName:          RuntimeNameVLLM,
		SKUNumGPUs:           1,
		NumNodes:             1,
		DistributedInference: false,
	}

	t.Run("balanced mode does not set --performance-mode flag", func(t *testing.T) {
		p := newPreset()
		rc := baseRC
		rc.RuntimeContextExtraArguments = RuntimeContextExtraArguments{PerformanceMode: "balanced"}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.NotContains(t, cmd[2], "performance-mode")
	})

	t.Run("empty mode does not set --performance-mode flag", func(t *testing.T) {
		p := newPreset()
		cmd := p.GetInferenceCommand(baseRC)
		require.Len(t, cmd, 3)
		assert.NotContains(t, cmd[2], "performance-mode")
	})

	t.Run("interactivity mode sets --performance-mode=interactivity", func(t *testing.T) {
		p := newPreset()
		rc := baseRC
		rc.RuntimeContextExtraArguments = RuntimeContextExtraArguments{PerformanceMode: "interactivity"}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "performance-mode=interactivity")
	})

	t.Run("throughput mode sets --performance-mode=throughput", func(t *testing.T) {
		p := newPreset()
		rc := baseRC
		rc.RuntimeContextExtraArguments = RuntimeContextExtraArguments{PerformanceMode: "throughput"}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "performance-mode=throughput")
	})
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

func TestModelFitsOnSingleGPU(t *testing.T) {
	tests := []struct {
		name                    string
		totalSafeTensorFileSize string
		modelFileSize           string
		gpuConfig               *sku.GPUConfig
		skuNumGPUs              int
		expected                bool
	}{
		{
			name:                    "model size < 50% single GPU memory, use data parallelism",
			totalSafeTensorFileSize: "8Gi",
			gpuConfig:               &sku.GPUConfig{GPUMem: resource.MustParse("160Gi")},
			skuNumGPUs:              4,
			expected:                true, // single GPU = 40Gi, 50% = 20Gi, 8Gi < 20Gi
		},
		{
			name:                    "model size == 50% single GPU memory, no data parallelism",
			totalSafeTensorFileSize: "20Gi",
			gpuConfig:               &sku.GPUConfig{GPUMem: resource.MustParse("160Gi")},
			skuNumGPUs:              4,
			expected:                false, // single GPU = 40Gi, 50% = 20Gi, 20Gi not < 20Gi
		},
		{
			name:                    "model size > 50% single GPU memory, no data parallelism",
			totalSafeTensorFileSize: "30Gi",
			gpuConfig:               &sku.GPUConfig{GPUMem: resource.MustParse("160Gi")},
			skuNumGPUs:              4,
			expected:                false, // single GPU = 40Gi, 50% = 20Gi, 30Gi > 20Gi
		},
		{
			name:          "falls back to ModelFileSize when TotalSafeTensorFileSize is empty",
			modelFileSize: "8Gi",
			gpuConfig:     &sku.GPUConfig{GPUMem: resource.MustParse("160Gi")},
			skuNumGPUs:    4,
			expected:      true,
		},
		{
			name:                    "no GPU config, returns false",
			totalSafeTensorFileSize: "8Gi",
			gpuConfig:               nil,
			skuNumGPUs:              4,
			expected:                false,
		},
		{
			name:                    "single GPU, returns false",
			totalSafeTensorFileSize: "8Gi",
			gpuConfig:               &sku.GPUConfig{GPUMem: resource.MustParse("80Gi")},
			skuNumGPUs:              1,
			expected:                false,
		},
		{
			name:       "no model file size, returns false",
			gpuConfig:  &sku.GPUConfig{GPUMem: resource.MustParse("160Gi")},
			skuNumGPUs: 4,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PresetParam{
				Metadata:                Metadata{ModelFileSize: tt.modelFileSize},
				TotalSafeTensorFileSize: tt.totalSafeTensorFileSize,
			}
			rc := RuntimeContext{
				GPUConfig:  tt.gpuConfig,
				SKUNumGPUs: tt.skuNumGPUs,
			}
			assert.Equal(t, tt.expected, p.modelFitsOnSingleGPU(rc))
		})
	}
}

func TestGetInferenceCommandVLLMDataParallelism(t *testing.T) {
	p := &PresetParam{
		TotalSafeTensorFileSize: "8Gi",
		RuntimeParam: RuntimeParam{
			VLLM: VLLMParam{
				BaseCommand:    "vllm serve",
				ModelRunParams: map[string]string{},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName: RuntimeNameVLLM,
		GPUConfig:   &sku.GPUConfig{GPUMem: resource.MustParse("320Gi")},
		SKUNumGPUs:  4,
		NumNodes:    1,
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "data-parallel-size=4")
	assert.Contains(t, cmd[2], "tensor-parallel-size=1")
}

func TestGetInferenceCommandVLLMDisableDataParallelism(t *testing.T) {
	// When DisableDataParallelism is true, even if model fits on a single GPU,
	// DP should be set to 1 and TP should be used instead.
	// This works around vLLM v0.17.x CUDA event device mismatch bug
	// (https://github.com/vllm-project/vllm/issues/37659).
	p := &PresetParam{
		TotalSafeTensorFileSize: "8Gi",
		RuntimeParam: RuntimeParam{
			DisableDataParallelism: true,
			VLLM: VLLMParam{
				BaseCommand:    "vllm serve",
				ModelRunParams: map[string]string{},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName: RuntimeNameVLLM,
		GPUConfig:   &sku.GPUConfig{GPUMem: resource.MustParse("320Gi")},
		SKUNumGPUs:  4,
		NumNodes:    1,
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "data-parallel-size=1")
	assert.Contains(t, cmd[2], "tensor-parallel-size=4")
}

func TestGetInferenceCommandVLLMTensorParallelismWhenModelLarge(t *testing.T) {
	p := &PresetParam{
		TotalSafeTensorFileSize: "64Gi",
		RuntimeParam: RuntimeParam{
			VLLM: VLLMParam{
				BaseCommand:    "vllm serve",
				ModelRunParams: map[string]string{},
			},
		},
	}
	rc := RuntimeContext{
		RuntimeName: RuntimeNameVLLM,
		GPUConfig:   &sku.GPUConfig{GPUMem: resource.MustParse("320Gi")},
		SKUNumGPUs:  4,
		NumNodes:    1,
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "tensor-parallel-size=4")
	assert.NotContains(t, cmd[2], "data-parallel-size")
}

func TestGetInferenceCommandVLLMMultiNodePPAndTP(t *testing.T) {
	// Tier 3: model too large for single node → PP across nodes + TP within node.
	p := &PresetParam{
		TotalSafeTensorFileSize: "256Gi",
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
		GPUConfig:            &sku.GPUConfig{GPUMem: resource.MustParse("320Gi")},
		SKUNumGPUs:           4,
		NumNodes:             2,
		DistributedInference: true,
		WorkspaceMetadata:    metav1.ObjectMeta{Name: "ws", Namespace: "default"},
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "tensor-parallel-size=4")
	assert.Contains(t, cmd[2], "pipeline-parallel-size=2")
	assert.NotContains(t, cmd[2], "data-parallel-size")
	assert.Contains(t, cmd[2], "POD_INDEX")
}

func TestGetInferenceCommandVLLMSmallModelOnMultiNodeUsesTP(t *testing.T) {
	// Even if model fits on single GPU, multi-node forces TP (no cross-node DP).
	p := &PresetParam{
		TotalSafeTensorFileSize: "8Gi",
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
		GPUConfig:            &sku.GPUConfig{GPUMem: resource.MustParse("320Gi")},
		SKUNumGPUs:           4,
		NumNodes:             2,
		DistributedInference: true,
		WorkspaceMetadata:    metav1.ObjectMeta{Name: "ws", Namespace: "default"},
	}
	cmd := p.GetInferenceCommand(rc)
	require.Len(t, cmd, 3)
	assert.Contains(t, cmd[2], "tensor-parallel-size=4")
	assert.Contains(t, cmd[2], "pipeline-parallel-size=2")
	assert.NotContains(t, cmd[2], "data-parallel-size")
}

func TestBuildVLLMInferenceCommandDTypeDynamic(t *testing.T) {
	t.Run("bfloat16 downgraded to float16 on older GPU", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{
					BaseCommand:    "vllm serve",
					ModelRunParams: map[string]string{"dtype": "bfloat16"},
				},
			},
		}
		rc := RuntimeContext{
			RuntimeName: RuntimeNameVLLM,
			SKUNumGPUs:  1,
			NumNodes:    1,
			GPUConfig:   &sku.GPUConfig{SKU: "test-t4", CUDAComputeCapability: 7.5},
		}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "dtype=float16")
		assert.NotContains(t, cmd[2], "dtype=bfloat16")
	})

	t.Run("bfloat16 preserved on Ampere GPU", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{
					BaseCommand:    "vllm serve",
					ModelRunParams: map[string]string{"dtype": "bfloat16"},
				},
			},
		}
		rc := RuntimeContext{
			RuntimeName: RuntimeNameVLLM,
			SKUNumGPUs:  1,
			NumNodes:    1,
			GPUConfig:   &sku.GPUConfig{SKU: "test-a100", CUDAComputeCapability: 8.0},
		}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "dtype=bfloat16")
	})

	t.Run("float16 unchanged on older GPU", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{
					BaseCommand:    "vllm serve",
					ModelRunParams: map[string]string{"dtype": "float16"},
				},
			},
		}
		rc := RuntimeContext{
			RuntimeName: RuntimeNameVLLM,
			SKUNumGPUs:  1,
			NumNodes:    1,
			GPUConfig:   &sku.GPUConfig{SKU: "test-t4", CUDAComputeCapability: 7.5},
		}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "dtype=float16")
	})

	t.Run("nil GPUConfig does not modify dtype", func(t *testing.T) {
		p := &PresetParam{
			RuntimeParam: RuntimeParam{
				VLLM: VLLMParam{
					BaseCommand:    "vllm serve",
					ModelRunParams: map[string]string{"dtype": "bfloat16"},
				},
			},
		}
		rc := RuntimeContext{
			RuntimeName: RuntimeNameVLLM,
			SKUNumGPUs:  1,
			NumNodes:    1,
			GPUConfig:   nil,
		}
		cmd := p.GetInferenceCommand(rc)
		require.Len(t, cmd, 3)
		assert.Contains(t, cmd[2], "dtype=bfloat16")
	})
}
