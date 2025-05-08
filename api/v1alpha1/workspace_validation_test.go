// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package v1alpha1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
var totalGPUMemoryRequirement string
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
		GPUCountRequirement:       gpuCountRequirement,
		TotalGPUMemoryRequirement: totalGPUMemoryRequirement,
		PerGPUMemoryRequirement:   perGPUMemoryRequirement,
	}
}
func (*testModel) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:       gpuCountRequirement,
		TotalGPUMemoryRequirement: totalGPUMemoryRequirement,
		PerGPUMemoryRequirement:   perGPUMemoryRequirement,
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
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "16Gi",
	}
}
func (*testModelStatic) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		GPUCountRequirement:       "1",
		TotalGPUMemoryRequirement: "16Gi",
		PerGPUMemoryRequirement:   "16Gi",
	}
}
func (*testModelStatic) SupportDistributedInference() bool {
	return false
}
func (*testModelStatic) SupportTuning() bool {
	return true
}

type testModelPrivate struct{}

func (*testModelPrivate) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		ImageAccessMode:           string(ModelImageAccessModePrivate),
		GPUCountRequirement:       gpuCountRequirement,
		TotalGPUMemoryRequirement: totalGPUMemoryRequirement,
		PerGPUMemoryRequirement:   perGPUMemoryRequirement,
	}
}
func (*testModelPrivate) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{
		ImageAccessMode:           string(ModelImageAccessModePrivate),
		GPUCountRequirement:       gpuCountRequirement,
		TotalGPUMemoryRequirement: totalGPUMemoryRequirement,
		PerGPUMemoryRequirement:   perGPUMemoryRequirement,
	}
}
func (*testModelPrivate) SupportDistributedInference() bool {
	return false
}
func (*testModelPrivate) SupportTuning() bool {
	return true
}

func RegisterValidationTestModels() {
	var test testModel
	var testPrivate testModelPrivate
	var testStatic testModelStatic
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-validation",
		Instance: &test,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "private-test-validation",
		Instance: &testPrivate,
	})
	plugin.KaitoModelRegister.Register(&plugin.Registration{
		Name:     "test-validation-static",
		Instance: &testStatic,
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
		name                string
		resourceSpec        *ResourceSpec
		modelGPUCount       string
		modelPerGPUMemory   string
		modelTotalGPUMemory string
		preset              bool
		presetNameOverride  string
		errContent          string // Content expect error to include, if any
		expectErrs          bool
		validateTuning      bool // To indicate if we are testing tuning validation
	}{
		{
			name: "Valid Resource",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_ND96asr_v4",
				Count:        pointerToInt(1),
			},
			modelGPUCount:       "8",
			modelPerGPUMemory:   "19Gi",
			modelTotalGPUMemory: "152Gi",
			preset:              true,
			errContent:          "",
			expectErrs:          false,
			validateTuning:      false,
		},
		{
			name: "Valid Resource - SKU Capacity == Model Requirement",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC12s_v3",
				Count:        pointerToInt(1),
			},
			modelGPUCount:       "1",
			modelPerGPUMemory:   "16Gi",
			modelTotalGPUMemory: "16Gi",
			preset:              true,
			errContent:          "",
			expectErrs:          false,
			validateTuning:      false,
		},
		{
			name: "Insufficient total GPU memory",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV6",
				Count:        pointerToInt(1),
			},
			modelGPUCount:       "1",
			modelPerGPUMemory:   "0",
			modelTotalGPUMemory: "14Gi",
			preset:              true,
			errContent:          "Insufficient total GPU memory",
			expectErrs:          true,
			validateTuning:      false,
		},

		{
			name: "Insufficient number of GPUs",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				Count:        pointerToInt(1),
			},
			modelGPUCount:       "2",
			modelPerGPUMemory:   "15Gi",
			modelTotalGPUMemory: "30Gi",
			preset:              true,
			errContent:          "Insufficient number of GPUs",
			expectErrs:          true,
			validateTuning:      false,
		},
		{
			name: "Insufficient per GPU memory",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV6",
				Count:        pointerToInt(2),
			},
			modelGPUCount:       "1",
			modelPerGPUMemory:   "15Gi",
			modelTotalGPUMemory: "15Gi",
			preset:              true,
			errContent:          "Insufficient per GPU memory",
			expectErrs:          true,
			validateTuning:      false,
		},

		{
			name: "Invalid SKU",
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_invalid_sku",
				Count:        pointerToInt(1),
			},
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
			expectErrs:         false,
		},
	}

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
				totalGPUMemoryRequirement = tc.modelTotalGPUMemory
				perGPUMemoryRequirement = tc.modelPerGPUMemory

				errs := tc.resourceSpec.validateCreateWithInference(&spec, false)
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
		{
			name: "Immutable InstanceType",
			newResource: &ResourceSpec{
				InstanceType: "new_type",
			},
			oldResource: &ResourceSpec{
				InstanceType: "old_type",
			},
			errContent: "field is immutable",
			expectErrs: true,
		},
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
			name: "Valid Update",
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
			errContent: "",
			expectErrs: false,
		},
	}

	// Run the tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
			name: "Private Access Without Image",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePrivate,
					},
					PresetOptions: PresetOptions{},
				},
			},
			errContent: "When AccessMode is private, an image must be provided",
			expectErrs: true,
		},
		{
			name: "Private Preset With Public AccessMode",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("private-test-validation"),
					},
					PresetOptions: PresetOptions{},
				},
			},
			errContent: "This preset only supports private AccessMode, AccessMode must be private to continue",
			expectErrs: true,
		},
		{
			name: "Adapeters more than 10",
			inferenceSpec: func() *InferenceSpec {
				spec := &InferenceSpec{
					Preset: &PresetSpec{
						PresetMeta: PresetMeta{
							Name:       ModelName("test-validation"),
							AccessMode: ModelImageAccessModePublic,
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
							Name:       ModelName("test-validation"),
							AccessMode: ModelImageAccessModePublic,
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
							Name:       ModelName("test-validation"),
							AccessMode: ModelImageAccessModePublic,
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
							Name:       ModelName("test-validation"),
							AccessMode: ModelImageAccessModePublic,
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
							Name:       ModelName("test-validation"),
							AccessMode: ModelImageAccessModePublic,
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
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePublic,
					},
				},
			},
			errContent: "Duplicate adapter source name found:",
			expectErrs: false,
		},
		{
			name: "Config specified but ConfigMap not found",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePublic,
					},
				},
				Config: "nonexistent-config",
			},
			errContent: "ConfigMap 'nonexistent-config' specified in 'config' not found in namespace",
			expectErrs: true,
		},
		{
			name: "Config specified with valid ConfigMap",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePublic,
					},
				},
				Config: "valid-config",
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name: "ConfigMap missing required inference_config.yaml",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePublic,
					},
				},
				Config: "missing-key-config",
			},
			errContent: "missing field(s): inference_config.yaml in ConfigMap",
			expectErrs: true,
		},
		{
			name: "ConfigMap missing required inference_config.yaml/transformers",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name:       ModelName("test-validation"),
						AccessMode: ModelImageAccessModePublic,
					},
				},
				Config: "missing-key-config",
			},
			runtimeName: model.RuntimeNameHuggingfaceTransformers,
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
			errs := tc.inferenceSpec.validateCreate(ctx, DefaultReleaseNamespace, "Standard_NC24ads_A100_v4", runtime)
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
			name:      "Neither Inference nor Tuning specified",
			workspace: &Workspace{},
			wantErr:   true,
			errField:  "neither",
		},
		{
			name: "Both Inference and Tuning specified",
			workspace: &Workspace{
				Inference: &InferenceSpec{},
				Tuning:    &TuningSpec{},
			},
			wantErr:  true,
			errField: "both",
		},
		{
			name: "Only Inference specified",
			workspace: &Workspace{
				Inference: &InferenceSpec{},
			},
			wantErr:  false,
			errField: "",
		},
		{
			name: "Only Tuning specified",
			workspace: &Workspace{
				Tuning: &TuningSpec{Input: &DataSource{}},
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

func TestWorkspaceValidateUpdate(t *testing.T) {
	tests := []struct {
		name         string
		oldWorkspace *Workspace
		newWorkspace *Workspace
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
				Tuning: &TuningSpec{Input: &DataSource{}},
			},
			newWorkspace: &Workspace{
				Tuning: &TuningSpec{Input: &DataSource{}},
			},
			expectErrs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.newWorkspace.validateUpdate(tt.oldWorkspace)
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
				Image: "kaito.azurecr.io/test:0.0.0",
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
		// {
		// 	name: "URLs and Volume specified",
		// 	dataSource: &DataSource{
		// 		URLs:   []string{"http://example.com/data"},
		// 		Volume: &v1.VolumeSource{},
		// 	},
		// 	wantErr:  true,
		// 	errField: "Exactly one of URLs, Volume, or Image must be specified",
		// },
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
			errField:        "At least one of Volume or Image must be specified",
		},
		// {
		// 	name: "Volume specified only",
		// 	dataDestination: &DataDestination{
		// 		Volume: &v1.VolumeSource{},
		// 	},
		// 	wantErr: false,
		// },
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
		// {
		// 	name: "Both fields specified",
		// 	dataDestination: &DataDestination{
		// 		Volume:          &v1.VolumeSource{},
		// 		Image:           "aimodels.azurecr.io/data-image:latest",
		// 		ImagePushSecret: "imagePushSecret",
		// 	},
		// 	wantErr: false,
		// },
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

	// Create fake client with test ConfigMaps
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
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
  gpu-memory-utilization: 0.95
`,
			},
		},
		// ConfigMap without max-model-len set
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-config-without-max-model-len",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm:
  gpu-memory-utilization: 0.95
`,
			},
		},
		// ConfigMap with empty vllm section
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-config-empty-vllm",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm: {}
`,
			},
		},
		// ConfigMap with vllm section missing
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-config-no-vllm",
				Namespace: DefaultReleaseNamespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
other_field: value
`,
			},
		},
	).Build()
	k8sclient.SetGlobalClient(client)

	tests := []struct {
		name          string
		inferenceSpec *InferenceSpec
		resourceSpec  *ResourceSpec
		runtimeName   model.RuntimeName
		errContent    string // Content expected error to include, if any
		expectErrs    bool
	}{
		{
			name: "Multi-GPU with <20GB per GPU and max-model-len set",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "valid-config-with-max-model-len",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
				Count:        pointerToInt(1),
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name: "Multi-GPU with <20GB per GPU and max-model-len missing",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "invalid-config-without-max-model-len",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
				Count:        pointerToInt(1),
			},
			errContent: "max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU",
			expectErrs: true,
		},
		{
			name: "Multi-GPU with <20GB per GPU and empty vllm section",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "invalid-config-empty-vllm",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
				Count:        pointerToInt(1),
			},
			errContent: "max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU",
			expectErrs: true,
		},
		{
			name: "Multi-GPU with <20GB per GPU and vllm section missing",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "invalid-config-no-vllm",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV12", // 2 GPUs with 8GB each (16GB total)
				Count:        pointerToInt(1),
			},
			errContent: "max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU",
			expectErrs: true,
		},
		{
			name: "Single-GPU instance (no max-model-len required)",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "invalid-config-without-max-model-len",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NV6", // 1 GPU with 8GB
				Count:        pointerToInt(1),
			},
			errContent: "",
			expectErrs: false,
		},
		{
			name: "Multi-GPU with >=20GB per GPU (no max-model-len required)",
			inferenceSpec: &InferenceSpec{
				Preset: &PresetSpec{
					PresetMeta: PresetMeta{
						Name: ModelName("test-validation"),
					},
				},
				Config: "invalid-config-without-max-model-len",
			},
			resourceSpec: &ResourceSpec{
				InstanceType: "Standard_NC48ads_A100_v4", // 2 GPUs with 80GB each (160GB total)
				Count:        pointerToInt(1),
			},
			errContent: "",
			expectErrs: false,
		},
	}

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Validate the inference spec
			runtimeName := model.RuntimeNameVLLM
			if tc.runtimeName != "" {
				runtimeName = tc.runtimeName
			}
			errs := tc.inferenceSpec.validateCreate(ctx, DefaultReleaseNamespace, tc.resourceSpec.InstanceType, runtimeName)
			hasErrs := errs != nil

			if hasErrs != tc.expectErrs {
				t.Errorf("validateCreate() errors = %v, expectErrs %v", errs, tc.expectErrs)
			}

			if hasErrs && tc.errContent != "" {
				errMsg := errs.Error()
				if !strings.Contains(errMsg, tc.errContent) {
					t.Errorf("validateCreate() error message = %v, expected to contain = %v", errMsg, tc.errContent)
				}
			}
		})
	}
}
