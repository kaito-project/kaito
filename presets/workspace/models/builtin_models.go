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

package models

import (
	_ "embed"
	"time"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

//go:embed builtin_models.yaml
var builtinModelsYAML []byte

// BuiltinCatalog holds the list of builtin models parsed from builtin_models.yaml.
type BuiltinCatalog struct {
	Models []BuiltinModelSpec `yaml:"models"`
}

// BuiltinModelSpec defines all metadata for a builtin preset model,
// fully deserializable from YAML. This replaces per-model Go registration files.
type BuiltinModelSpec struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Version string `yaml:"version"`

	// Resource sizing
	TotalSafeTensorFileSize string `yaml:"totalSafeTensorFileSize"`
	DiskStorageRequirement  string `yaml:"diskStorageRequirement"`
	GPUCountRequirement     string `yaml:"gpuCountRequirement"`

	// KV cache / context window
	BytesPerToken   int `yaml:"bytesPerToken"`
	ModelTokenLimit int `yaml:"modelTokenLimit"`

	// Startup
	ReadinessTimeout time.Duration `yaml:"readinessTimeout"`

	// Feature flags
	SupportDistributedInference bool `yaml:"supportDistributedInference"`
	SupportTuning               bool `yaml:"supportTuning"`

	// Optional flags for edge-case models
	DisableTensorParallelism bool `yaml:"disableTensorParallelism,omitempty"`

	// Runtime parameters for inference
	RuntimeParameters BuiltinRuntimeParameters `yaml:"runtimeParameters"`

	// Tuning parameters (optional, only if supportTuning is true)
	Tuning *BuiltinTuningSpec `yaml:"tuning,omitempty"`
}

// BuiltinRuntimeParameters holds configuration for each supported runtime engine.
type BuiltinRuntimeParameters struct {
	Transformers *BuiltinTransformersParam `yaml:"transformers,omitempty"`
	VLLM         *BuiltinVLLMParam         `yaml:"vllm,omitempty"`
}

// BuiltinTransformersParam defines Transformers runtime configuration.
type BuiltinTransformersParam struct {
	BaseCommand       string            `yaml:"baseCommand"`
	InferenceMainFile string            `yaml:"inferenceMainFile,omitempty"`
	AccelerateParams  map[string]string `yaml:"accelerateParams,omitempty"`
	ModelRunParams    map[string]string `yaml:"modelRunParams,omitempty"`
}

// BuiltinVLLMParam defines vLLM runtime configuration.
type BuiltinVLLMParam struct {
	BaseCommand          string            `yaml:"baseCommand"`
	ModelRunParams       map[string]string `yaml:"modelRunParams,omitempty"`
	DisallowLoRA         bool              `yaml:"disallowLoRA,omitempty"`
	RayLeaderBaseCommand string            `yaml:"rayLeaderBaseCommand,omitempty"`
	RayWorkerBaseCommand string            `yaml:"rayWorkerBaseCommand,omitempty"`
}

// BuiltinTuningSpec defines tuning-specific overrides.
type BuiltinTuningSpec struct {
	TotalSafeTensorFileSize       string                         `yaml:"totalSafeTensorFileSize"`
	DiskStorageRequirement        string                         `yaml:"diskStorageRequirement"`
	GPUCountRequirement           string                         `yaml:"gpuCountRequirement"`
	ReadinessTimeout              time.Duration                  `yaml:"readinessTimeout"`
	TuningPerGPUMemoryRequirement map[string]int                 `yaml:"tuningPerGPUMemoryRequirement,omitempty"`
	RuntimeParameters             BuiltinTuningRuntimeParameters `yaml:"runtimeParameters"`
}

// BuiltinTuningRuntimeParameters holds tuning-specific runtime configuration.
type BuiltinTuningRuntimeParameters struct {
	Transformers *BuiltinTuningTransformersParam `yaml:"transformers,omitempty"`
}

// BuiltinTuningTransformersParam defines Transformers runtime configuration for tuning.
type BuiltinTuningTransformersParam struct {
	BaseCommand string `yaml:"baseCommand"`
}

// builtinModel implements the model.Model interface using a BuiltinModelSpec
// loaded from YAML, eliminating the need for per-model Go files.
type builtinModel struct {
	spec BuiltinModelSpec
}

func (m *builtinModel) GetInferenceParameters() *model.PresetParam {
	metadata := model.Metadata{
		Name:              m.spec.Name,
		ModelType:         m.spec.Type,
		Version:           m.spec.Version,
		DownloadAtRuntime: true,
	}

	param := &model.PresetParam{
		Metadata:                metadata,
		DiskStorageRequirement:  m.spec.DiskStorageRequirement,
		GPUCountRequirement:     m.spec.GPUCountRequirement,
		TotalSafeTensorFileSize: m.spec.TotalSafeTensorFileSize,
		BytesPerToken:           m.spec.BytesPerToken,
		ModelTokenLimit:         m.spec.ModelTokenLimit,
		ReadinessTimeout:        m.spec.ReadinessTimeout,
	}

	param.RuntimeParam.DisableTensorParallelism = m.spec.DisableTensorParallelism

	// Transformers runtime
	if t := m.spec.RuntimeParameters.Transformers; t != nil {
		param.Transformers = model.HuggingfaceTransformersParam{
			BaseCommand:       t.BaseCommand,
			InferenceMainFile: t.InferenceMainFile,
			AccelerateParams:  t.AccelerateParams,
			ModelRunParams:    t.ModelRunParams,
			ModelName:         m.spec.Name,
		}
	}

	// vLLM runtime
	if v := m.spec.RuntimeParameters.VLLM; v != nil {
		param.VLLM = model.VLLMParam{
			BaseCommand:          v.BaseCommand,
			ModelName:            m.spec.Name,
			ModelRunParams:       v.ModelRunParams,
			DisallowLoRA:         v.DisallowLoRA,
			RayLeaderBaseCommand: v.RayLeaderBaseCommand,
			RayWorkerBaseCommand: v.RayWorkerBaseCommand,
		}
		// Set distributed inference defaults for Ray commands
		if m.spec.SupportDistributedInference {
			if param.VLLM.RayLeaderBaseCommand == "" {
				param.VLLM.RayLeaderBaseCommand = DefaultVLLMRayLeaderBaseCommand
			}
			if param.VLLM.RayWorkerBaseCommand == "" {
				param.VLLM.RayWorkerBaseCommand = DefaultVLLMRayWorkerBaseCommand
			}
		}
	}

	return param
}

func (m *builtinModel) GetTuningParameters() *model.PresetParam {
	if !m.spec.SupportTuning || m.spec.Tuning == nil {
		return nil
	}

	t := m.spec.Tuning
	metadata := model.Metadata{
		Name:              m.spec.Name,
		ModelType:         m.spec.Type,
		Version:           m.spec.Version,
		DownloadAtRuntime: true,
	}

	param := &model.PresetParam{
		Metadata:                      metadata,
		DiskStorageRequirement:        t.DiskStorageRequirement,
		GPUCountRequirement:           t.GPUCountRequirement,
		TotalSafeTensorFileSize:       t.TotalSafeTensorFileSize,
		TuningPerGPUMemoryRequirement: t.TuningPerGPUMemoryRequirement,
		ReadinessTimeout:              t.ReadinessTimeout,
	}

	if tt := t.RuntimeParameters.Transformers; tt != nil {
		param.Transformers = model.HuggingfaceTransformersParam{
			BaseCommand: tt.BaseCommand,
			ModelName:   m.spec.Name,
		}
	}

	return param
}

func (m *builtinModel) SupportDistributedInference() bool {
	return m.spec.SupportDistributedInference
}

func (m *builtinModel) SupportTuning() bool {
	return m.spec.SupportTuning
}

// registerBuiltinModels parses builtin_models.yaml and registers each model
// in the plugin registry.
func registerBuiltinModels() {
	catalog := BuiltinCatalog{}
	utilruntime.Must(yaml.Unmarshal(builtinModelsYAML, &catalog))

	for _, spec := range catalog.Models {
		m := &builtinModel{spec: spec}
		plugin.KaitoModelRegister.Register(&plugin.Registration{
			Name:     spec.Name,
			Instance: m,
		})
	}
}
