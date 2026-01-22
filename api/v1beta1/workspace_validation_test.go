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

package v1beta1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

const DefaultReleaseNamespace = "kaito-workspace"

var ValidStrength string = "0.5"
var InvalidStrength1 string = "invalid"
var InvalidStrength2 string = "1.5"

var gpuCountRequirement string
var totalSafeTensorFileSize string
var perGPUMemoryRequirement string

var invalidSourceName string

func init() {
	// Define a invalid source name longer than 253
	for i := 0; i < 32; i++ {
		invalidSourceName += "Adapter1"
	}
}

type testModel struct{}

func (*testModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     gpuCountRequirement,
		TotalSafeTensorFileSize: totalSafeTensorFileSize,
		ModelTokenLimit:         4096, // Add ModelTokenLimit for validation testing
	}
}
func (*testModel) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     gpuCountRequirement,
		TotalSafeTensorFileSize: totalSafeTensorFileSize,
	}
}
func (*testModel) SupportDistributedInference() bool {
	return false
}
func (*testModel) SupportTuning() bool {
	return true
}

type testModelStatic struct{}

func (*testModelStatic) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "16Gi",
	}
}
func (*testModelStatic) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "16Gi",
	}
}
func (*testModelStatic) SupportDistributedInference() bool {
	return false
}
func (*testModelStatic) SupportTuning() bool {
	return true
}

type testModelDownload struct{}

func (*testModelDownload) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata: model.Metadata{
			Version:              "https://huggingface.co/test-repo/test-model/commit/test-revision",
			DownloadAtRuntime:    true,
			DownloadAuthRequired: true,
		},
		GPUCountRequirement:     "2",
		TotalSafeTensorFileSize: "32Gi",
	}
}
func (*testModelDownload) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*testModelDownload) SupportDistributedInference() bool {
	return true
}
func (*testModelDownload) SupportTuning() bool {
	return false
}

// Represents a large model that requires significant resources
type testModelLarge struct{}

func (*testModelLarge) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     "8",     // Requires 8 GPUs
		TotalSafeTensorFileSize: "131Gi", // Approximately 131GB total memory requirement
	}
}
func (*testModelLarge) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*testModelLarge) SupportDistributedInference() bool {
	return true // Supports distributed inference across multiple nodes
}
func (*testModelLarge) SupportTuning() bool {
	return false
}

// Represents a smaller model that fits on a single A10 GPU
type testModelSmallA10 struct{}

func (*testModelSmallA10) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:     "1",   // Requires 1 GPU
		TotalSafeTensorFileSize: "7Gi", // Small model that fits on A10
	}
}
func (*testModelSmallA10) GetTuningParameters() *model.PresetParam {
	return nil
}
func (*testModelSmallA10) SupportDistributedInference() bool {
	return false // Does not support distributed inference
}
func (*testModelSmallA10) SupportTuning() bool {
	return false
}

func RegisterValidationTestModels() {
	var test testModel
	var testStatic testModelStatic
	var testDownload testModelDownload
	var testLarge testModelLarge
	var testSmallA10 testModelSmallA10
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-validation",
		Instance: &test,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-validation-static",
		Instance: &testStatic,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-validation-download",
		Instance: &testDownload,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-large-model",
		Instance: &testLarge,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-small-a10",
		Instance: &testSmallA10,
	})
}

func pointerToInt(i int) *int {
	return &i
}

func defaultConfigMapManifest() *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultLoraConfigMapTemplate,
			Namespace: DefaultReleaseNamespace, // Replace this with the appropriate namespace variable if dynamic
		},
		Data: map[string]string{
			"training_config.yaml": `training_config:
  ModelConfig:
    torch_dtype: "bfloat16"
    local_files_only: true
    device_map: "auto"

  QuantizationConfig:
    load_in_4bit: false

  LoraConfig:
    r: 16
    lora_alpha: 32
    target_modules: "query_key_value"
    lora_dropout: 0.05
    bias: "none"

  TrainingArguments:
    output_dir: "output"
    num_train_epochs: 4
    auto_find_batch_size: true
    ddp_find_unused_parameters: false
    save_strategy: "epoch"

  DatasetConfig:
    shuffle_dataset: true
    train_test_split: 1

  DataCollator:
    mlm: true`,
		},
	}
}

func qloraConfigMapManifest() *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultQloraConfigMapTemplate,
			Namespace: DefaultReleaseNamespace,
		},
		Data: map[string]string{
			"training_config.yaml": `training_config:
  ModelConfig:
    torch_dtype: "bfloat16"
    local_files_only: true
    device_map: "auto"

  QuantizationConfig:
    load_in_4bit: true
    bnb_4bit_quant_type: "nf4"
    bnb_4bit_compute_dtype: "bfloat16"
    bnb_4bit_use_double_quant: true

  LoraConfig:
    r: 16
    lora_alpha: 32
    target_modules: "query_key_value"
    lora_dropout: 0.05
    bias: "none"

  TrainingArguments:
    output_dir: "output"
    num_train_epochs: 4
    auto_find_batch_size: true
    ddp_find_unused_parameters: false
    save_strategy: "epoch"

  DatasetConfig:
    shuffle_dataset: true
    train_test_split: 1

  DataCollator:
    mlm: true`,
		},
	}
}

func defaultInferenceConfigMapManifest() *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultInferenceConfigTemplate,
			Namespace: DefaultReleaseNamespace,
		},
		Data: map[string]string{
			"inference_config.yaml": `# Maximum number of steps to find the max available seq len fitting in the GPU memory.
max_probe_steps: 6

vllm:
  cpu-offload-gb: 0
  gpu-memory-utilization: 0.95
  swap-space: 4

  # max-seq-len-to-capture: 8192
  # num-scheduler-steps: 1
  # enable-chunked-prefill: false
  # max-model-len: 2048
  # see https://docs.vllm.ai/en/stable/serving/engine_args.html for more options.`,
		},
	}
}

func TestResourceSpecValidateCreate(t *testing.T) {
	RegisterValidationTestModels()
	tests := []struct {
		name                    string
		resourceSpec            *ResourceSpec
		modelGPUCount           string
		modelPerGPUMemory       string
		totalSafeTensorFileSize string
		preset                  bool
		presetNameOverride      string
		runtime                 model.RuntimeName
		errContent              string // Content expect error to include, if any
		expectErrs              bool
		validateTuning          bool      // To indicate if we are testing tuning validation
		testNodes               []v1.Node // Test nodes for BYO scenarios
		useFeatureGate          bool      // Whether to enable BYO feature gate
	}{
		{
			name: "Valid Resource",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_ND96asr_v4",
				Count:        pointerToInt(1),
			},
			modelGPUCount:           "8",
			modelPerGPUMemory:       "19Gi",
			totalSafeTensorFileSize: "152Gi",
			preset:                  true,
			runtime:                 model.RuntimeNameVLLM,
			errContent:              "",
			expectErrs:              false,
			validateTuning:          false,
		},
		{
			name: "Valid Resource - SKU Capacity == Model Requirement",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC12s_v3",
				Count:        pointerToInt(1),
			},
			modelGPUCount:           "1",
			modelPerGPUMemory:       "16Gi",
			totalSafeTensorFileSize: "16Gi",
			preset:                  true,
			runtime:                 model.RuntimeNameVLLM,
			errContent:              "",
			expectErrs:              false,
			validateTuning:          false,
		},
		{
			name: "Invalid SKU",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_invalid_sku",
				Count:        pointerToInt(1),
			},
			runtime:        model.RuntimeNameVLLM,
			errContent:     "Unsupported instance",
			expectErrs:     true,
			validateTuning: false,
		},
		{
			name: "Only Template set",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV12s_v3",
				Count:        pointerToInt(1),
			},
			preset:         false,
			runtime:        model.RuntimeNameVLLM,
			errContent:     "",
			expectErrs:     false,
			validateTuning: false,
		},
		{
			name: "N-Prefix SKU",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_Nsku",
				Count:        pointerToInt(1),
			},
			runtime:        model.RuntimeNameVLLM,
			errContent:     "",
			expectErrs:     false,
			validateTuning: false,
		},

		{
			name: "D-Prefix SKU",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_Dsku",
				Count:        pointerToInt(1),
			},
			runtime:        model.RuntimeNameVLLM,
			errContent:     "",
			expectErrs:     false,
			validateTuning: false,
		},
		{
			name: "Tuning validation with single node",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(1),
			},
			runtime:        model.RuntimeNameVLLM,
			errContent:     "",
			expectErrs:     false,
			validateTuning: true,
		},
		{
			name: "Tuning validation with multinode",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(2),
			},
			runtime:        model.RuntimeNameVLLM,
			errContent:     "Tuning does not currently support multinode configurations",
			expectErrs:     true,
			validateTuning: true,
		},
		{
			name: "Invalid Preset Name",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(2),
			},
			errContent:         "",
			preset:             true,
			presetNameOverride: "Invalid-Preset-Name",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         false,
		},
		{
			name: "vLLM + Distributed Inference",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(4),
			},
			preset:             true,
			presetNameOverride: "test-validation-download",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         false,
		},
		// BYO (Bring Your Own) Node Tests - moved from TestValidateBYONodes
		{
			name: "Valid single A100 node with sufficient GPU memory",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "a100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static", // 16Gi requirement
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         false,
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-a100-1",
						Labels: map[string]string{
							"gpu":                    "a100",
							"nvidia.com/gpu.product": "NVIDIA-A100-SXM4-80GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "81920", // 80GB * 1024 = 81920 MiB
						},
					},
				},
			},
			useFeatureGate: true, // Enable BYO mode
		},
		{
			name: "Valid multiple H100 nodes with uniform configuration",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(2),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "h100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static", // 16Gi requirement
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         false,
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-h100-1",
						Labels: map[string]string{
							"gpu":                    "h100",
							"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-94GB",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "96256", // 94GB * 1024 = 96256 MiB
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-h100-2",
						Labels: map[string]string{
							"gpu":                    "h100",
							"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-94GB",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "96256", // 94GB * 1024 = 96256 MiB
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - no nodes matching label selector in BYO",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "nonexistent",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "No nodes found matching the specified label selector",
			testNodes:          []v1.Node{}, // No nodes available
			useFeatureGate:     true,
		},
		{
			name: "Invalid - missing GPU product label",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "v100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "Failed to get GPU config from nvidia labels",
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-missing-gpu-product",
						Labels: map[string]string{
							"gpu":                   "v100",
							"nvidia.com/gpu.count":  "1",
							"nvidia.com/gpu.memory": "16384", // 16GB * 1024 = 16384 MiB
							// Missing nvidia.com/gpu.product
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - missing GPU count label",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "v100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "Failed to get GPU config from nvidia labels",
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-missing-gpu-count",
						Labels: map[string]string{
							"gpu":                    "v100",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.memory":  "16384", // 16GB * 1024 = 16384 MiB
							// Missing nvidia.com/gpu.count
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - missing GPU memory label",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "v100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "Failed to get GPU config from nvidia labels",
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-missing-gpu-memory",
						Labels: map[string]string{
							"gpu":                    "v100",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.count":   "1",
							// Missing nvidia.com/gpu.memory
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - non-uniform GPU products",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(2),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"workload": "gpu",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "Non-uniform GPU product",
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-v100",
						Labels: map[string]string{
							"workload":               "gpu",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "16384", // 16GB * 1024 = 16384 MiB
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-a100",
						Labels: map[string]string{
							"workload":               "gpu",
							"nvidia.com/gpu.product": "NVIDIA-A100-SXM4-80GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "81920", // 80GB * 1024 = 81920 MiB
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - insufficient GPU memory for non-distributed model",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "t4",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",                 // Needs 16Gi, doesn't support distributed
			runtime:            model.RuntimeNameHuggingfaceTransformers, // Use non-vLLM runtime to get resource validation
			expectErrs:         true,
			errContent:         "Insufficient total GPU memory",
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-t4",
						Labels: map[string]string{
							"gpu":                    "t4",
							"nvidia.com/gpu.product": "NVIDIA-T4-16GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "8192", // Only 8GB * 1024 = 8192 MiB, but model needs 16Gi
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Valid - distributed model on nodes with sufficient combined memory",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(2),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"gpu": "v100",
					},
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-download", // Supports distributed inference
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         false,
			testNodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-v100-1",
						Labels: map[string]string{
							"gpu":                    "v100",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "16384", // 16GB * 1024 = 16384 MiB
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-v100-2",
						Labels: map[string]string{
							"gpu":                    "v100",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "16384", // 16GB * 1024 = 16384 MiB
						},
					},
				},
			},
			useFeatureGate: true,
		},
		{
			name: "Invalid - missing labelSelector for BYO mode",
			resourceSpec: &ResourceSpec{
				InstanceType: "", // Empty instanceType indicates BYO mode
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{}, // Empty match labels instead of nil
				},
			},
			preset:             true,
			presetNameOverride: "test-validation-static",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "No nodes found matching the specified label selector",
			testNodes:          []v1.Node{},
			useFeatureGate:     true,
		},
		{
			name: "Deprecated Model",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(1),
			},
			preset:             true,
			presetNameOverride: "phi-2",
			runtime:            model.RuntimeNameVLLM,
			expectErrs:         true,
			errContent:         "Model phi-2 is deprecated and no longer supported",
		},
	}

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set up BYO feature gate if needed
			if tc.useFeatureGate {
				// Enable BYO mode by setting the feature gate
				originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
				defer func() {
					featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
				}()
			}

			// Set up fake client with test nodes if provided
			if len(tc.testNodes) > 0 || tc.useFeatureGate {
				scheme := runtime.NewScheme()
				_ = v1.AddToScheme(scheme)

				// Convert []v1.Node to []runtime.Object
				objects := make([]runtime.Object, len(tc.testNodes))
				for i, node := range tc.testNodes {
					nodeCopy := node.DeepCopy()
					objects[i] = nodeCopy
				}

				client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
				k8sclient.SetGlobalClient(client)
			}

			if tc.validateTuning {
				tuningSpec := &TuningSpec{}
				errs := tc.resourceSpec.validateCreateWithTuning(tuningSpec)
				hasErrs := errs != nil
				if hasErrs != tc.expectErrs {
					t.Errorf("validateCreateWithTuning() errors = %v, expectErrs %v", errs, tc.expectErrs)
				}

				if hasErrs && tc.errContent != "" {
					errMsg := errs.Error()
					if !strings.Contains(errMsg, tc.errContent) {
						t.Errorf("validateCreateWithTuning() error message = %v, expected to contain = %v", errMsg, tc.errContent)
					}
				}
			} else {
				var spec InferenceSpec

				if tc.preset {
					presetName := ModelName("test-validation")
					if tc.presetNameOverride != "" {
						presetName = ModelName(tc.presetNameOverride)
					}

					spec = InferenceSpec{
						Preset: &PresetSpec{
							PresetMeta: PresetMeta{
								Name: presetName,
							},
						},
					}
				} else {
					spec = InferenceSpec{
						Template: &v1.PodTemplateSpec{}, // Assuming a non-nil TemplateSpec implies it's set
					}
				}

				gpuCountRequirement = tc.modelGPUCount
				totalSafeTensorFileSize = tc.totalSafeTensorFileSize
				perGPUMemoryRequirement = tc.modelPerGPUMemory

				errs := tc.resourceSpec.validateCreateWithInference(context.TODO(), &spec, false, tc.runtime, "")
				hasErrs := errs != nil
				if hasErrs != tc.expectErrs {
					t.Errorf("validateCreate() errors = %v, expectErrs %v", errs, tc.expectErrs)
				}

				// If there is an error and errContent is not empty, check that the error contains the expected content.
				if hasErrs && tc.errContent != "" {
					errMsg := errs.Error()
					if !strings.Contains(errMsg, tc.errContent) {
						t.Errorf("validateCreate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
					}
				}
			}
		})
	}
}

func TestResourceSpecValidateUpdate(t *testing.T) {

	tests := []struct {
		name        string
		newResource *ResourceSpec
		oldResource *ResourceSpec
		disableNAP  bool   // Whether to disable Node Auto-Provisioning
		errContent  string // Content expected error to include, if any
		expectErrs  bool
	}{
		{
			name: "Immutable Count",
			newResource: &ResourceSpec{
				Count: pointerToInt(10),
			},
			oldResource: &ResourceSpec{
				Count: pointerToInt(5),
			},
			errContent: "field is immutable",
			expectErrs: true,
		},
		// NAP Enabled Cases
		{
			name: "NAP enabled - change instanceType (invalid)",
			newResource: &ResourceSpec{
				InstanceType: "new_type",
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "old_type",
				Count:        pointerToInt(1),
			},
			disableNAP: false, // NAP enabled
			errContent: "instanceType is cannot be changed once set when node auto-provisioning is enabled",
			expectErrs: true,
		},
		{
			name: "NAP enabled - clear instanceType (invalid)",
			newResource: &ResourceSpec{
				InstanceType: "", // Trying to clear instanceType
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3",
				Count:        pointerToInt(1),
			},
			disableNAP: false, // NAP enabled
			errContent: "instanceType is required when node auto-provisioning is enabled",
			expectErrs: true,
		},
		{
			name: "NAP enabled - set instanceType initially (valid)",
			newResource: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3", // Setting for first time
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "", // Was empty
				Count:        pointerToInt(1),
			},
			disableNAP: false, // NAP enabled
			errContent: "",
			expectErrs: false,
		},
		{
			name: "NAP enabled - keep same instanceType (valid)",
			newResource: &ResourceSpec{
				InstanceType: "same_type",
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "same_type",
				Count:        pointerToInt(1),
			},
			disableNAP: false, // NAP enabled
			errContent: "",
			expectErrs: false,
		},
		// NAP Disabled Cases
		{
			name: "NAP disabled - clear instanceType (valid upgrade path)",
			newResource: &ResourceSpec{
				InstanceType: "", // Clearing instanceType
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"gpu": "v100"},
				},
			},
			oldResource: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3", // Had instanceType from v0.7
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"gpu": "v100"}, // Same labelSelector
				},
			},
			disableNAP: true, // NAP disabled (BYO mode)
			errContent: "",
			expectErrs: false,
		},
		{
			name: "NAP disabled - keep instanceType set (backward compatibility)",
			newResource: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3", // Still has instanceType
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "Standard_NC6s_v3", // Had instanceType from v0.7
				Count:        pointerToInt(1),
			},
			disableNAP: true, // NAP disabled (BYO mode)
			expectErrs: false,
		},
		{
			name: "NAP disabled - change to different instanceType (invalid)",
			newResource: &ResourceSpec{
				InstanceType: "new_type", // Changing instanceType
				Count:        pointerToInt(1),
			},
			oldResource: &ResourceSpec{
				InstanceType: "old_type",
				Count:        pointerToInt(1),
			},
			disableNAP: true, // NAP disabled (BYO mode)
			errContent: "instanceType is cannot be changed once set",
			expectErrs: true,
		},
		{
			name: "NAP disabled - instanceType already empty (valid)",
			newResource: &ResourceSpec{
				InstanceType: "",
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"gpu": "v100"},
				},
			},
			oldResource: &ResourceSpec{
				InstanceType: "",
				Count:        pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"gpu": "v100"},
				},
			},
			disableNAP: true, // NAP disabled (BYO mode)
			errContent: "",
			expectErrs: false,
		},
		// LabelSelector immutability
		{
			name: "Immutable LabelSelector",
			newResource: &ResourceSpec{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key1": "value1"}},
			},
			oldResource: &ResourceSpec{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key2": "value2"}},
			},
			errContent: "field is immutable",
			expectErrs: true,
		},
		{
			name: "Valid Update with all fields unchanged",
			newResource: &ResourceSpec{
				Count:         pointerToInt(5),
				InstanceType:  "same_type",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}},
			},
			oldResource: &ResourceSpec{
				Count:         pointerToInt(5),
				InstanceType:  "same_type",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}},
			},
			disableNAP: false, // NAP enabled
			errContent: "",
			expectErrs: false,
		},
	}

	// Run the tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set or reset the feature gate if specified
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.disableNAP
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
			}()

			errs := tc.newResource.validateUpdate(tc.oldResource)
			hasErrs := errs != nil
			if hasErrs != tc.expectErrs {
				t.Errorf("validateUpdate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			// If there is an error and errContent is not empty, check that the error contains the expected content.
			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateUpdate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}

func TestInferenceSpecValidateCreate(t *testing.T) {
	RegisterValidationTestModels()
	ctx := context.Background()

	// Set environment variables
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client with default ConfigMap
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		defaultInferenceConfigMapManifest(),
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-config",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": "a: b",
			},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "missing-key-config",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"other_key": "some value",
			},
		},
	).Build()
	k8sclient.SetGlobalClient(client)

	tests := []struct {
		name          string
		inferenceSpec *InferenceSpec
		runtimeName   model.RuntimeName
		errContent    string // Content expected error to include, if any
		expectErrs    bool
	}{
		{
			name: "Invalid Preset Name",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("Invalid-Preset-Name"),
					},
				},
			},
			errContent: "Unsupported inference preset name",
			expectErrs: true,
		},
		{
			name: "Only Template set",
			inferenceSpec: &InferenceSpec{
				Template: &v1.PodTemplateSpec{}, // Assuming a non-nil TemplateSpec implies it's set
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name:          "Preset and Template Unset",
			inferenceSpec: &InferenceSpec{},
			errContent:    "Preset or Template must be specified",
			expectErrs:    true,
		},
		{
			name: "Preset and Template Set",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Template: &v1.PodTemplateSpec{}, // Assuming a non-nil TemplateSpec implies it's set
			},
			errContent: "Preset and Template cannot be set at the same time",
			expectErrs: true,
		},
		{
			name: "Adapeters more than 10",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
				}
				for i := 1; i <= 11; i++ {
					spec.Adapters = append(spec.Adapters, AdapterSpec{
						Source: &DataSource{
							Name:  fmt.Sprintf("Adapter-%d", i),
							Image: fmt.Sprintf("fake.kaito.com/kaito-image:0.0.%d", i),
						},
						Strength: &ValidStrength,
					})
				}
				return spec
			}(),
			errContent: "Number of Adapters exceeds the maximum limit, maximum of 10 allowed",
			expectErrs: true,
		},
		{
			name: "Valid with adapters/vllm",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
				}
				for i := 1; i <= 2; i++ {
					spec.Adapters = append(spec.Adapters, AdapterSpec{
						Source: &DataSource{
							Name:  fmt.Sprintf("Adapter-%d", i),
							Image: fmt.Sprintf("fake.kaito.com/kaito-image:0.0.%d", i),
						},
					})
				}
				return spec
			}(),
		},
		{
			name: "Valid with adapters/transformers",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
				}
				for i := 1; i <= 2; i++ {
					spec.Adapters = append(spec.Adapters, AdapterSpec{
						Source: &DataSource{
							Name:  fmt.Sprintf("Adapter-%d", i),
							Image: fmt.Sprintf("fake.kaito.com/kaito-image:0.0.%d", i),
						},
						Strength: &ValidStrength,
					})
				}
				return spec
			}(),
			runtimeName: model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name: "Adapters with strength/vllm",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
				}
				for i := 1; i <= 2; i++ {
					spec.Adapters = append(spec.Adapters, AdapterSpec{
						Source: &DataSource{
							Name:  fmt.Sprintf("Adapter-%d", i),
							Image: fmt.Sprintf("fake.kaito.com/kaito-image:0.0.%d", i),
						},
						Strength: &ValidStrength,
					})
				}
				return spec
			}(),
			errContent: "vLLM does not support adapter strength",
			expectErrs: true,
		},
		{
			name: "Adapters names are duplicated",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
				}
				for i := 1; i <= 2; i++ {
					spec.Adapters = append(spec.Adapters, AdapterSpec{
						Source: &DataSource{
							Name:  "Adapter",
							Image: fmt.Sprintf("fake.kaito.com/kaito-image:0.0.%d", i),
						},
						Strength: &ValidStrength,
					})
				}
				return spec
			}(),
			errContent: "",
			expectErrs: true,
		},
		{
			name: "Valid Preset",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
			},
			errContent: "Duplicate adapter source name found:",
			expectErrs: false,
		},
		{
			name: "Config specified with valid ConfigMap",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "valid-config",
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name: "download model at runtime with access secret",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation-download"),
					},
					PresetOptions: PresetOptions{
						ModelAccessSecret: "test-secret",
					},
				},
			},
		},
		{
			name: "download model at runtime but no access secret",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation-download"),
					},
				},
			},
			errContent: "This preset requires authentication and needs a modelAccessSecret with HF_TOKEN key under presetOptions to download the model",
			expectErrs: true,
		},
		{
			name: "Preset with model weights packaged but with access secret",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
					PresetOptions: PresetOptions{
						ModelAccessSecret: "test-secret",
					},
				},
			},
			errContent: "This preset does not require a modelAccessSecret with HF_TOKEN key under presetOptions",
			expectErrs: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set CLOUD_PROVIDER environment variable for all test cases
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			// If the test expects an error, setup defer function to catch the panic.
			if tc.expectErrs {
				defer func() {
					if r := recover(); r != nil {
						// Check if the recovered panic matches the expected error content.
						if errContent, ok := r.(string); ok && strings.Contains(errContent, tc.errContent) {
							return
						}
						t.Errorf("unexpected panic: %v", r)
					}
				}()
			}
			runtime := model.RuntimeNameVLLM
			if tc.runtimeName != "" {
				runtime = tc.runtimeName
			}
			errs := tc.inferenceSpec.validateCreate(ctx, runtime, "")
			hasErrs := errs != nil
			if hasErrs != tc.expectErrs {
				t.Errorf("validateCreate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			// If there is an error and errContent is not empty, check that the error contains the expected content.
			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateCreate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}

func TestAdapterSpecValidateCreateorUpdate(t *testing.T) {
	RegisterValidationTestModels()
	tests := []struct {
		name        string
		adapterSpec *AdapterSpec
		errContent  string // Content expected error to include, if any
		expectErrs  bool
	}{
		{
			name: "Missing Source",
			adapterSpec: &AdapterSpec{
				Strength: &ValidStrength,
			},
			errContent: "Source",
			expectErrs: true,
		},
		{
			name: "Missing Source Name",
			adapterSpec: &AdapterSpec{
				Source: &DataSource{
					Image: "fake.kaito.com/kaito-image:0.0.1",
				},
				Strength: &ValidStrength,
			},
			errContent: "Name of Adapter field must be specified",
			expectErrs: true,
		},
		{
			name: "Invalid Strength, not a number",
			adapterSpec: &AdapterSpec{
				Source: &DataSource{
					Name:  "Adapter-1",
					Image: "fake.kaito.com/kaito-image:0.0.1",
				},
				Strength: &InvalidStrength1,
			},
			errContent: "Invalid strength value for Adapter 'Adapter-1'",
			expectErrs: true,
		},
		{
			name: "Invalid Strength, larger than 1",
			adapterSpec: &AdapterSpec{
				Source: &DataSource{
					Name:  "Adapter-1",
					Image: "fake.kaito.com/kaito-image:0.0.1",
				},
				Strength: &InvalidStrength2,
			},
			errContent: "Strength value for Adapter 'Adapter-1' must be between 0 and 1",
			expectErrs: true,
		},
		{
			name: "Invalid Source Name, longer than 253",
			adapterSpec: &AdapterSpec{
				Source: &DataSource{
					Name:  invalidSourceName,
					Image: "fake.kaito.com/kaito-image:0.0.1",
				},
				Strength: &ValidStrength,
			},
			errContent: "invalid value",
			expectErrs: true,
		},
		{
			name: "Valid Adapter",
			adapterSpec: &AdapterSpec{
				Source: &DataSource{
					Name:  "adapter-1",
					Image: "fake.kaito.com/kaito-image:0.0.1",
				},
			},
			errContent: "",
			expectErrs: false,
		},
	}

	// Run the tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.adapterSpec.validateCreateorUpdate()
			hasErrs := errs != nil
			if hasErrs != tc.expectErrs {
				t.Errorf("validateUpdate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			// If there is an error and errContent is not empty, check that the error contains the expected content.
			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateUpdate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}

func TestInferenceSpecValidateUpdate(t *testing.T) {
	tests := []struct {
		name         string
		newInference *InferenceSpec
		oldInference *InferenceSpec
		errContent   string // Content expected error to include, if any
		expectErrs   bool
	}{
		{
			name: "Preset Immutable",
			newInference: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("new-preset"),
					},
				},
			},
			oldInference: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("old-preset"),
					},
				},
			},
			errContent: "field is immutable",
			expectErrs: true,
		},
		{
			name: "Template Unset",
			newInference: &InferenceSpec{
				Template: nil,
			},
			oldInference: &InferenceSpec{
				Template: &v1.PodTemplateSpec{},
			},
			errContent: "field cannot be unset/set if it was set/unset",
			expectErrs: true,
		},
		{
			name: "Template Set",
			newInference: &InferenceSpec{
				Template: &v1.PodTemplateSpec{},
			},
			oldInference: &InferenceSpec{
				Template: nil,
			},
			errContent: "field cannot be unset/set if it was set/unset",
			expectErrs: true,
		},
		{
			name: "Template Set",
			newInference: &InferenceSpec{
				Template: &v1.PodTemplateSpec{},
				Adapters: []AdapterSpec{
					{
						Source: &DataSource{
							Name:  "Adapter-1",
							Image: "fake.kaito.com/kaito-image:0.0.1",
						},
					},
					{
						Source: &DataSource{
							Name:  "Adapter-1",
							Image: "fake.kaito.com/kaito-image:0.0.6",
						},
					},
				},
			},
			oldInference: &InferenceSpec{
				Template: nil,
			},
			errContent: "field cannot be unset/set if it was set/unset",
			expectErrs: true,
		},
		{
			name: "Valid Update",
			newInference: &InferenceSpec{
				Template: &v1.PodTemplateSpec{},
			},
			oldInference: &InferenceSpec{
				Template: &v1.PodTemplateSpec{},
			},
			errContent: "",
			expectErrs: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.newInference.validateUpdate(tc.oldInference)
			hasErrs := errs != nil
			if hasErrs != tc.expectErrs {
				t.Errorf("validateUpdate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			// If there is an error and errContent is not empty, check that the error contains the expected content.
			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateUpdate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}

func TestWorkspaceValidateCreate(t *testing.T) {
	tests := []struct {
		name      string
		workspace *Workspace
		wantErr   bool
		errField  string
	}{
		{
			name: "Neither Inference nor Tuning specified",
			workspace: &Workspace{
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			wantErr:  true,
			errField: "neither",
		},
		{
			name: "Both Inference and Tuning specified",
			workspace: &Workspace{
				Inference: &InferenceSpec{},
				Tuning:    &TuningSpec{},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			wantErr:  true,
			errField: "both",
		},
		{
			name: "Only Inference specified",
			workspace: &Workspace{
				Inference: &InferenceSpec{},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			wantErr:  false,
			errField: "",
		},
		{
			name: "Only Tuning specified",
			workspace: &Workspace{
				Tuning: &TuningSpec{Input: &DataSource{}},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			wantErr:  false,
			errField: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.workspace.validateCreate()
			if (errs != nil) != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", errs, tt.wantErr)
			}
			if errs != nil && !strings.Contains(errs.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain field %s, but got %s", tt.errField, errs.Error())
			}
		})
	}
}

func TestWorkspaceValidateName(t *testing.T) {
	RegisterValidationTestModels()

	// Set environment variables
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client with default ConfigMap
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		defaultInferenceConfigMapManifest(),
	).Build()
	k8sclient.SetGlobalClient(client)

	testWorkspace := &Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testWorkspace",
			Namespace: "kaito",
		},
		Resource: ResourceSpec{
			InstanceType: "Standard_NC6s_v3",
			Count:        pointerToInt(1),
		},
		Inference: &InferenceSpec{
			Preset: &PresetSpec{
				PresetMeta: PresetMeta{
					Name: ModelName("test-validation-static"),
				},
			},
		},
	}

	tests := []struct {
		name          string
		workspaceName string
		wantErr       bool
		errField      string
	}{
		{
			name:          "Valid name",
			workspaceName: "valid-name",
			wantErr:       false,
		},
		{
			name:          "Name with invalid characters",
			workspaceName: "phi-3.5-mini",
			wantErr:       true,
			errField:      "name",
		},
		{
			name:          "Name start with invalid character",
			workspaceName: "-mini",
			wantErr:       true,
			errField:      "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := testWorkspace.DeepCopy()
			workspace.Name = tt.workspaceName
			errs := workspace.Validate(context.Background())
			if (errs != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", errs, tt.wantErr)
			}
			if errs != nil && !strings.Contains(errs.Error(), tt.errField) {
				t.Errorf("Validate() expected error to contain field %s, but got %s", tt.errField, errs.Error())
			}
		})
	}
}

func TestWorkspaceValidateNAPFeatureGate(t *testing.T) {
	RegisterValidationTestModels()

	// Set environment variables
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client with default ConfigMap
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		defaultInferenceConfigMapManifest(),
	).Build()
	k8sclient.SetGlobalClient(client)

	tests := []struct {
		name        string
		workspace   *Workspace
		disableNAP  bool // Whether to disable Node Auto-Provisioning
		expectErrs  bool
		errContains string
	}{
		{
			name: "NAP enabled - instanceType required",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace-nap-enabled",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "", // Missing instanceType when NAP enabled
					Count:        pointerToInt(1),
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			disableNAP:  false, // NAP enabled
			expectErrs:  true,
			errContains: "instanceType is required when node auto-provisioning is enabled",
		},
		{
			name: "NAP enabled - instanceType provided (valid)",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace-nap-enabled-valid",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3", // Valid instanceType when NAP enabled
					Count:        pointerToInt(1),
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			disableNAP:  false, // NAP enabled
			expectErrs:  false,
			errContains: "",
		},
		{
			name: "NAP disabled - instanceType must be empty",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace-nap-disabled",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3", // Invalid: instanceType provided when NAP disabled
					Count:        pointerToInt(1),
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"gpu": "v100",
						},
					},
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			disableNAP:  true, // NAP disabled (BYO mode)
			expectErrs:  true,
			errContains: "instanceType must be empty when node auto-provisioning is disabled (BYO scenario)",
		},
		{
			name: "NAP disabled - instanceType empty (valid)",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace-nap-disabled-valid",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "", // Valid: empty instanceType when NAP disabled
					Count:        pointerToInt(1),
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"gpu": "v100",
						},
					},
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			disableNAP:  true, // NAP disabled (BYO mode)
			expectErrs:  false,
			errContains: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set or reset the feature gate
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.disableNAP
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
			}()

			// For BYO scenarios, set up fake nodes if needed
			if tc.disableNAP && tc.workspace.Resource.LabelSelector != nil {
				// Create a test node that matches the label selector
				testNode := v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"gpu":                    "v100",
							"nvidia.com/gpu.product": "NVIDIA-V100-SXM2-16GB",
							"nvidia.com/gpu.count":   "1",
							"nvidia.com/gpu.memory":  "16384",
						},
					},
				}

				// Create new fake client with the test node
				scheme := runtime.NewScheme()
				_ = v1.AddToScheme(scheme)
				client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
					defaultInferenceConfigMapManifest(),
					&testNode,
				).Build()
				k8sclient.SetGlobalClient(client)
			}

			// Call the full Validate method
			errs := tc.workspace.Validate(context.Background())
			hasErrs := errs != nil

			if hasErrs != tc.expectErrs {
				t.Errorf("Validate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			if hasErrs && tc.errContains != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContains) {
					t.Errorf("Validate() error message = %v, expected to contain = %v", errMsg, tc.errContains)
				}
			}
		})
	}
}

func TestWorkspaceValidateUpdate(t *testing.T) {
	RegisterValidationTestModels()

	// Set environment variables
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		defaultInferenceConfigMapManifest(),
	).Build()
	k8sclient.SetGlobalClient(client)

	tests := []struct {
		name         string
		oldWorkspace *Workspace
		newWorkspace *Workspace
		disableNAP   bool // Whether to disable Node Auto-Provisioning
		expectErrs   bool
		errFields    []string // Fields we expect to have errors
	}{
		{
			name:         "Inference toggled on",
			oldWorkspace: &Workspace{},
			newWorkspace: &Workspace{
				Inference: &InferenceSpec{},
			},
			expectErrs: true,
			errFields:  []string{"inference"},
		},
		{
			name: "Inference toggled off",
			oldWorkspace: &Workspace{
				Inference: &InferenceSpec{Preset: &PresetSpec{}},
			},
			newWorkspace: &Workspace{},
			expectErrs:   true,
			errFields:    []string{"inference"},
		},
		{
			name:         "Tuning toggled on",
			oldWorkspace: &Workspace{},
			newWorkspace: &Workspace{
				Tuning: &TuningSpec{Input: &DataSource{}},
			},
			expectErrs: true,
			errFields:  []string{"tuning"},
		},
		{
			name: "Tuning toggled off",
			oldWorkspace: &Workspace{
				Tuning: &TuningSpec{Input: &DataSource{}},
			},
			newWorkspace: &Workspace{},
			expectErrs:   true,
			errFields:    []string{"tuning"},
		},
		{
			name: "No toggling",
			oldWorkspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "kaito",
				},
				Tuning: &TuningSpec{Input: &DataSource{}, Output: &DataDestination{Image: "test-image:latest", ImagePushSecret: "secret"}},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			newWorkspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "kaito",
				},
				Tuning: &TuningSpec{Input: &DataSource{}, Output: &DataDestination{Image: "test-image:latest", ImagePushSecret: "secret"}},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3",
					Count:        pointerToInt(1),
				},
			},
			expectErrs: false,
		},
		{
			name: "Upgrade scenario - v0.7 workspace with instanceType when disableNAP=true",
			oldWorkspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v07-workspace",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "Standard_NC6s_v3", // Created in v0.7 with instanceType
					Count:        pointerToInt(1),
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"gpu": "nvidia",
						},
					},
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			newWorkspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v07-workspace",
					Namespace: "kaito",
				},
				Resource: ResourceSpec{
					InstanceType: "", // CLEARED when upgrading to v0.8 with disableNAP=true
					Count:        pointerToInt(1),
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"gpu": "nvidia",
						},
					},
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation-static"),
						},
					},
				},
			},
			disableNAP: true,  // Upgrading to v0.8 with disableNAP=true
			expectErrs: false, // Should NOT fail during UPDATE when clearing instanceType
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set or reset the feature gate if specified
			if tt.disableNAP {
				originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
				defer func() {
					featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
				}()
			}

			// Call Validate with baseline to simulate UPDATE operation
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, tt.oldWorkspace)
			errs := tt.newWorkspace.Validate(ctx)
			hasErrs := errs != nil

			if hasErrs != tt.expectErrs {
				t.Errorf("validateUpdate() errors = %v, expectErrs %v", errs, tt.expectErrs)
			}

			if hasErrs {
				for _, field := range tt.errFields {
					if !strings.Contains(errs.Error(), field) {
						t.Errorf("validateUpdate() expected errors to contain field %s, but got %s", field, errs.Error())
					}
				}
			}
		})
	}
}

func TestTuningSpecValidateCreate(t *testing.T) {
	RegisterValidationTestModels()
	// Set ReleaseNamespace Env
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client with default ConfigMap
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(defaultConfigMapManifest(), qloraConfigMapManifest()).Build()
	k8sclient.SetGlobalClient(client)
	// Include client in ctx
	ctx := context.Background()

	tests := []struct {
		name       string
		tuningSpec *TuningSpec
		wantErr    bool
		errFields  []string // Fields we expect to have errors
	}{
		{
			name: "All fields valid",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input", Image: "kaito.azurecr.io/test:0.0.0"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: "secret"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			wantErr:   false,
			errFields: nil,
		},
		{
			name: "Verify QLoRA Config",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input", Image: "kaito.azurecr.io/test:0.0.0"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: "secret"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodQLora,
			},
			wantErr:   false,
			errFields: nil,
		},
		{
			name: "Missing Input",
			tuningSpec: &TuningSpec{
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: ""},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"Input"},
		},
		{
			name: "Missing Output",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"Output"},
		},
		{
			name: "Missing Preset",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: ""},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"Preset"},
		},
		{
			name: "Invalid Preset",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: ""},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("invalid-preset")}},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"presetName"},
		},
		{
			name: "Invalid Method",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0", ImagePushSecret: ""},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: "invalid-method",
			},
			wantErr:   true,
			errFields: []string{"Method"},
		},
		{
			name: "Invalid Input Source Casing",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input", Image: "kaito.azurecr.io/INPUT:0.0.0"},
				Output: &DataDestination{Image: "kaito.azurecr.io/output:0.0.0", ImagePushSecret: "secret"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"Image"},
		},
		{
			name: "Invalid Output Destination Casing",
			tuningSpec: &TuningSpec{
				Input:  &DataSource{Name: "valid-input", Image: "kaito.azurecr.io/input:0.0.0"},
				Output: &DataDestination{Image: "kaito.azurecr.io/OUTPUT:0.0.0", ImagePushSecret: "secret"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			wantErr:   true,
			errFields: []string{"Image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.tuningSpec.validateCreate(ctx, "WORKSPACE_NAMESPACE")
			hasErrs := errs != nil

			if hasErrs != tt.wantErr {
				t.Errorf("validateCreate() errors = %v, wantErr %v", errs, tt.wantErr)
			}

			if hasErrs {
				for _, field := range tt.errFields {
					if !strings.Contains(errs.Error(), field) {
						t.Errorf("validateCreate() expected errors to contain field %s, but got %s", field, errs.Error())
					}
				}
			}
		})
	}
}

func TestTuningSpecValidateUpdate(t *testing.T) {
	RegisterValidationTestModels()
	tests := []struct {
		name       string
		oldTuning  *TuningSpec
		newTuning  *TuningSpec
		expectErrs bool
		errFields  []string // Fields we expect to have errors
	}{
		{
			name: "No changes",
			oldTuning: &TuningSpec{
				Input:  &DataSource{Name: "input1"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			newTuning: &TuningSpec{
				Input:  &DataSource{Name: "input1"},
				Output: &DataDestination{Image: "kaito.azurecr.io/test:0.0.0"},
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
				Method: TuningMethodLora,
			},
			expectErrs: false,
		},
		{
			name: "Preset changed",
			oldTuning: &TuningSpec{
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-validation")}},
			},
			newTuning: &TuningSpec{
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("invalid-preset")}},
			},
			expectErrs: true,
			errFields:  []string{"Preset"},
		},
		{
			name: "Method changed",
			oldTuning: &TuningSpec{
				Method: TuningMethodLora,
			},
			newTuning: &TuningSpec{
				Method: TuningMethodQLora,
			},
			expectErrs: true,
			errFields:  []string{"Method"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.newTuning.validateUpdate(tt.oldTuning)
			hasErrs := errs != nil

			if hasErrs != tt.expectErrs {
				t.Errorf("validateUpdate() errors = %v, expectErrs %v", errs, tt.expectErrs)
			}

			if hasErrs {
				for _, field := range tt.errFields {
					if !strings.Contains(errs.Error(), field) {
						t.Errorf("validateUpdate() expected errors to contain field %s, but got %s", field, errs.Error())
					}
				}
			}
		})
	}
}

func TestDataSourceValidateCreate(t *testing.T) {
	tests := []struct {
		name       string
		dataSource *DataSource
		wantErr    bool
		errField   string // The field we expect to have an error on
	}{
		{
			name: "URLs specified only",
			dataSource: &DataSource{
				URLs: []string{"http://example.com/data"},
			},
			wantErr: false,
		},
		{
			name: "Volume specified only",
			dataSource: &DataSource{
				Volume: &v1.VolumeSource{},
			},
			wantErr: false,
		},
		{
			name: "Image specified only",
			dataSource: &DataSource{
				Image: "aimodels.azurecr.io/data-image:latest",
			},
			wantErr: false,
		},
		{
			name: "Image without URL Specified",
			dataSource: &DataSource{
				Image:            "data-image:latest",
				ImagePullSecrets: []string{"imagePushSecret"},
			},
			wantErr: false,
		},
		{
			name: "Image without Tag Specified",
			dataSource: &DataSource{
				Image:            "aimodels.azurecr.io/data-image",
				ImagePullSecrets: []string{"imagePushSecret"},
			},
			wantErr: false,
		},
		{
			name: "Invalid image",
			dataSource: &DataSource{
				Image:            "!ValidImage",
				ImagePullSecrets: []string{"imagePushSecret"},
			},
			wantErr:  true,
			errField: "invalid reference format",
		},
		{
			name:       "None specified",
			dataSource: &DataSource{},
			wantErr:    true,
			errField:   "Exactly one of URLs, Volume, or Image must be specified",
		},
		{
			name: "URLs and Volume specified",
			dataSource: &DataSource{
				URLs:   []string{"http://example.com/data"},
				Volume: &v1.VolumeSource{},
			},
			wantErr:  true,
			errField: "Exactly one of URLs, Volume, or Image must be specified",
		},
		{
			name: "All fields specified",
			dataSource: &DataSource{
				URLs:  []string{"http://example.com/data"},
				Image: "aimodels.azurecr.io/data-image:latest",
			},
			wantErr:  true,
			errField: "Exactly one of URLs, Volume, or Image must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.dataSource.validateCreate()
			hasErrs := errs != nil

			if hasErrs != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", errs, tt.wantErr)
			}

			if hasErrs && tt.errField != "" && !strings.Contains(errs.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, errs.Error())
			}
		})
	}
}

func TestDataSourceValidateUpdate(t *testing.T) {
	tests := []struct {
		name      string
		oldSource *DataSource
		newSource *DataSource
		wantErr   bool
		errFields []string // Fields we expect to have errors
	}{
		{
			name: "No changes",
			oldSource: &DataSource{
				URLs: []string{"http://example.com/data1", "http://example.com/data2"},
				// Volume:           &v1.VolumeSource{},
				Image:            "data-image:latest",
				ImagePullSecrets: []string{"secret1", "secret2"},
			},
			newSource: &DataSource{
				URLs: []string{"http://example.com/data2", "http://example.com/data1"}, // Note the different order, should not matter
				// Volume:           &v1.VolumeSource{},
				Image:            "data-image:latest",
				ImagePullSecrets: []string{"secret2", "secret1"}, // Note the different order, should not matter
			},
			wantErr: false,
		},
		{
			name: "Name changed",
			oldSource: &DataSource{
				Name: "original-dataset",
			},
			newSource: &DataSource{
				Name: "new-dataset",
			},
			wantErr:   true,
			errFields: []string{"Name"},
		},
		{
			name:      "Invalid image",
			oldSource: &DataSource{},
			newSource: &DataSource{
				Image:            "!ValidImage",
				ImagePullSecrets: []string{"imagePushSecret"},
			},
			wantErr:   true,
			errFields: []string{"invalid reference format"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.newSource.validateUpdate(tt.oldSource, true)
			hasErrs := errs != nil

			if hasErrs != tt.wantErr {
				t.Errorf("validateUpdate() error = %v, wantErr %v", errs, tt.wantErr)
			}

			if hasErrs {
				for _, field := range tt.errFields {
					if !strings.Contains(errs.Error(), field) {
						t.Errorf("validateUpdate() expected errors to contain field %s, but got %s", field, errs.Error())
					}
				}
			}
		})
	}
}

func TestDataDestinationValidateCreate(t *testing.T) {
	tests := []struct {
		name            string
		dataDestination *DataDestination
		wantErr         bool
		errField        string // The field we expect to have an error on
	}{
		{
			name:            "No fields specified",
			dataDestination: &DataDestination{},
			wantErr:         true,
			errField:        "Exactly one of Volume or Image must be specified",
		},
		{
			name: "Volume specified only",
			dataDestination: &DataDestination{
				Volume: &v1.VolumeSource{},
			},
			wantErr: false,
		},
		{
			name: "Image specified only",
			dataDestination: &DataDestination{
				Image:           "aimodels.azurecr.io/data-image:latest",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr: false,
		},
		{
			name: "Image without URL Specified",
			dataDestination: &DataDestination{
				Image:           "data-image:latest",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr: false,
		},
		{
			name: "Image without Tag Specified",
			dataDestination: &DataDestination{
				Image:           "aimodels.azurecr.io/data-image",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr: false,
		},
		{
			name: "Invalid image",
			dataDestination: &DataDestination{
				Image:           "!ValidImage",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr:  true,
			errField: "invalid reference format",
		},
		{
			name: "Both fields specified",
			dataDestination: &DataDestination{
				Volume:          &v1.VolumeSource{},
				Image:           "aimodels.azurecr.io/data-image:latest",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr:  true,
			errField: "Exactly one of Volume or Image must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.dataDestination.validateCreate()
			hasErrs := errs != nil

			if hasErrs != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", errs, tt.wantErr)
			}

			if hasErrs && tt.errField != "" && !strings.Contains(errs.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, errs.Error())
			}
		})
	}
}

func TestDataDestinationValidateUpdate(t *testing.T) {
	tests := []struct {
		name      string
		oldDest   *DataDestination
		newDest   *DataDestination
		wantErr   bool
		errFields []string // Fields we expect to have errors
	}{
		{
			name: "No changes",
			oldDest: &DataDestination{
				// Volume:          &v1.VolumeSource{},
				Image:           "old-image:latest",
				ImagePushSecret: "old-secret",
			},
			newDest: &DataDestination{
				// Volume:          &v1.VolumeSource{},
				Image:           "old-image:latest",
				ImagePushSecret: "old-secret",
			},
			wantErr: false,
		},
		{
			name:    "Invalid image",
			oldDest: &DataDestination{},
			newDest: &DataDestination{
				Image:           "!ValidImage",
				ImagePushSecret: "imagePushSecret",
			},
			wantErr:   true,
			errFields: []string{"invalid reference format"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.newDest.validateUpdate()
			hasErrs := errs != nil

			if hasErrs != tt.wantErr {
				t.Errorf("validateUpdate() error = %v, wantErr %v", errs, tt.wantErr)
			}

			if hasErrs {
				for _, field := range tt.errFields {
					if !strings.Contains(errs.Error(), field) {
						t.Errorf("validateUpdate() expected errors to contain field %s, but got %s", field, errs.Error())
					}
				}
			}
		})
	}
}

func TestInferenceConfigMapValidation(t *testing.T) {
	RegisterValidationTestModels()
	ctx := context.Background()

	// Set environment variables
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv(consts.DefaultReleaseNamespaceEnvVar, DefaultReleaseNamespace)

	// Create fake client with test ConfigMaps
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		// ConfigMap with max-model-len that exceeds ModelTokenLimit (20480 > 4096)
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-config-exceeds-token-limit",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm:
  max-model-len: 20480
  gpu-memory-utilization: 0.84
`,
			},
		},
		// ConfigMap with max-model-len set
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-config-with-max-model-len",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm:
  max-model-len: 2048
  gpu-memory-utilization: 0.84
`,
			},
		},
	).Build()
	k8sclient.SetGlobalClient(client)

	tests := []struct {
		name       string
		workspace  *Workspace
		errContent string // Content expected error to include, if any
		expectErrs bool
	}{
		{
			name: "Valid max-model-len within ModelTokenLimit",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: DefaultReleaseNamespace,
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
					Config: "valid-config-with-max-model-len",
				},
				Resource: ResourceSpec{
					InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
					Count:        pointerToInt(1),
				},
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name: "max-model-len exceeds ModelTokenLimit",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: DefaultReleaseNamespace,
				},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name: ModelName("test-validation"),
						},
					},
					Config: "invalid-config-exceeds-token-limit",
				},
				Resource: ResourceSpec{
					InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
					Count:        pointerToInt(1),
				},
			},
			errContent: "max-model-len 20480 exceeds model's maximum supported context window 4096 (ModelTokenLimit)",
			expectErrs: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Call validateInferenceConfig directly
			errs := tc.workspace.validateInferenceConfig(ctx)
			hasErrs := errs != nil

			if hasErrs != tc.expectErrs {
				t.Errorf("validateInferenceConfig() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateInferenceConfig() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}
