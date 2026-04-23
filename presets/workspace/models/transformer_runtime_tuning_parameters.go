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
	"time"

	"github.com/kaito-project/kaito/pkg/model"
)

// TuningConfig holds the tuning-specific parameters that are centralized
// for each model that supports tuning.
type TuningConfig struct {
	DiskStorageRequirement        string
	GPUCountRequirement           string
	TotalSafeTensorFileSize       string
	ModelTokenLimit               int
	BytesPerToken                 int
	TuningPerGPUMemoryRequirement map[string]int
	Transformers                  model.HuggingfaceTransformersParam
	ReadinessTimeout              time.Duration
}

const defaultTuningBaseCommand = "cd /workspace/tfs/ && python3 metrics_server.py & accelerate launch"

// TransformerTuningParameters maps preset model names to their tuning
// configuration. Only models that support tuning have entries here.
// Each model's GetTuningParameters() should look up its config from this map
// and construct the full PresetParam on the fly.
var TransformerTuningParameters = map[string]TuningConfig{
	// Phi-4 family
	"phi-4": {
		DiskStorageRequirement:  "150Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "70Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-4",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"phi-4-mini-instruct": {
		DiskStorageRequirement:  "70Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "72Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-4-mini-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},

	// Phi-3 family
	"phi-3-mini-4k-instruct": {
		DiskStorageRequirement:  "80Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "72Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-3-mini-4k-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"phi-3-mini-128k-instruct": {
		DiskStorageRequirement:  "80Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "72Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-3-mini-128k-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"phi-3-medium-4k-instruct": {
		DiskStorageRequirement:  "120Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "80Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-3-medium-4k-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"phi-3-medium-128k-instruct": {
		DiskStorageRequirement:  "120Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "80Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-3-medium-128k-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"phi-3.5-mini-instruct": {
		DiskStorageRequirement:  "70Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "72Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "phi-3.5-mini-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},

	// Mistral family
	"mistral-7b-v0.3": {
		DiskStorageRequirement:  "90Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "16Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "mistral-7b",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},

	// Qwen family
	"qwen2.5-coder-7b-instruct": {
		DiskStorageRequirement:  "110Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "24Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "qwen2.5-coder-7b-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
	"qwen2.5-coder-32b-instruct": {
		DiskStorageRequirement:  "230Gi",
		GPUCountRequirement:     "1",
		TotalSafeTensorFileSize: "140Gi",
		Transformers: model.HuggingfaceTransformersParam{
			BaseCommand: defaultTuningBaseCommand,
			ModelName:   "qwen2.5-coder-32b-instruct",
		},
		ReadinessTimeout: time.Duration(30) * time.Minute,
	},
}
