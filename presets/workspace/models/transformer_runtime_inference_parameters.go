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
	"github.com/kaito-project/kaito/pkg/model"
)

// Defaults for Transformers inference, mirrored from pkg/workspace/inference to
// avoid an import cycle (pkg/workspace/inference already imports this package).
var (
	defaultAccelerateParams = map[string]string{
		"num_processes": "1",
		"num_machines":  "1",
		"machine_rank":  "0",
		"gpu_ids":       "all",
	}
	defaultTransformersMainFile = "/workspace/tfs/inference_api.py"
)

// Shared ModelRunParams for model families that use the same parameters.
var (
	phiRunParams = map[string]string{
		"torch_dtype":       "auto",
		"pipeline":          "text-generation",
		"trust_remote_code": "",
	}
	mistralRunParams = map[string]string{
		"torch_dtype":   "bfloat16",
		"pipeline":      "text-generation",
		"chat_template": "/workspace/chat_templates/mistral-instruct.jinja",
	}
	gemma3RunParams = map[string]string{
		"torch_dtype":        "auto",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	qwenRunParams = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	llamaRunParams = map[string]string{
		"torch_dtype":        "bfloat16",
		"pipeline":           "text-generation",
		"chat_template":      "/workspace/chat_templates/llama-3-instruct.jinja",
		"allow_remote_files": "",
	}
	gptRunParams = map[string]string{
		"torch_dtype":        "auto",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
	falconRunParams = map[string]string{
		"torch_dtype":   "bfloat16",
		"pipeline":      "text-generation",
		"chat_template": "/workspace/chat_templates/falcon-instruct.jinja",
	}
	deepseekDistillRunParams = map[string]string{
		"torch_dtype": "bfloat16",
		"pipeline":    "text-generation",
	}
	deepseekLargeRunParams = map[string]string{
		"torch_dtype":        "bfloat16",
		"pipeline":           "text-generation",
		"allow_remote_files": "",
	}
)

// TransformerInferenceParameters maps preset model names to their Huggingface
// Transformers runtime parameters for inference. Each model's
// GetInferenceParameters() should look up its Transformers config from this map
// instead of hardcoding the values inline.
var TransformerInferenceParameters = map[string]model.HuggingfaceTransformersParam{
	// Phi-4 family
	"phi-4": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-4",
		Tag:               "0.2.0",
	},
	"phi-4-mini-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-4-mini-instruct",
		Tag:               "0.2.0",
	},

	// Phi-3 family
	"phi-3-mini-4k-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-3-mini-4k-instruct",
		Tag:               "0.2.0",
	},
	"phi-3-mini-128k-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-3-mini-128k-instruct",
		Tag:               "0.2.0",
	},
	"phi-3-medium-4k-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-3-medium-4k-instruct",
		Tag:               "0.2.0",
	},
	"phi-3-medium-128k-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-3-medium-128k-instruct",
		Tag:               "0.2.0",
	},
	"phi-3.5-mini-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    phiRunParams,
		ModelName:         "phi-3.5-mini-instruct",
		Tag:               "0.2.0",
	},

	// Mistral family
	"mistral-7b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "mistral-7b",
		Tag:               "0.2.0",
	},
	"mistral-7b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "mistral-7b-instruct",
		Tag:               "0.2.0",
	},
	"ministral-3-3b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "ministral-3-3b-instruct",
		Tag:               "0.0.1",
	},
	"ministral-3-8b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "ministral-3-8b-instruct",
		Tag:               "0.0.1",
	},
	"ministral-3-14b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "ministral-3-14b-instruct",
		Tag:               "0.0.1",
	},
	"mistral-large-3-675b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    mistralRunParams,
		ModelName:         "mistral-large-3-675b-instruct",
		Tag:               "0.0.1",
	},

	// Gemma-3 family
	"gemma-3-4b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    gemma3RunParams,
		ModelName:         "gemma-3-4b-instruct",
		Tag:               "0.0.1",
	},
	"gemma-3-27b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    gemma3RunParams,
		ModelName:         "gemma-3-27b-instruct",
		Tag:               "0.0.1",
	},

	// Qwen family
	"qwen2.5-coder-7b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    qwenRunParams,
		ModelName:         "qwen2.5-coder-7b-instruct",
		Tag:               "0.2.0",
	},
	"qwen2.5-coder-32b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    qwenRunParams,
		ModelName:         "qwen2.5-coder-32b-instruct",
		Tag:               "0.2.0",
	},

	// Llama-3 family
	"llama-3.1-8b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    llamaRunParams,
		ModelName:         "llama-3.1-8b-instruct",
		Tag:               "0.2.0",
	},
	"llama-3.3-70b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    llamaRunParams,
		ModelName:         "llama-3.3-70b-instruct",
		Tag:               "0.0.1",
	},

	// GPT-OSS family
	"gpt-oss-20b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    gptRunParams,
		ModelName:         "gpt-oss-20b",
		Tag:               "0.0.2",
	},
	"gpt-oss-120b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    gptRunParams,
		ModelName:         "gpt-oss-120b",
		Tag:               "0.0.1",
	},

	// Falcon family
	"falcon-7b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    falconRunParams,
		ModelName:         "falcon-7b",
		Tag:               "0.2.0",
	},
	"falcon-7b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    falconRunParams,
		ModelName:         "falcon-7b-instruct",
		Tag:               "0.2.0",
	},
	"falcon-40b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    falconRunParams,
		ModelName:         "falcon-40b",
		Tag:               "0.2.0",
	},
	"falcon-40b-instruct": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    falconRunParams,
		ModelName:         "falcon-40b-instruct",
		Tag:               "0.2.0",
	},

	// DeepSeek family
	"deepseek-r1-distill-llama-8b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    deepseekDistillRunParams,
		ModelName:         "deepseek-r1-distill-llama-8b",
		Tag:               "0.2.0",
	},
	"deepseek-r1-distill-qwen-14b": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    deepseekDistillRunParams,
		ModelName:         "deepseek-r1-distill-qwen-14b",
		Tag:               "0.2.0",
	},
	"deepseek-r1-0528": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    deepseekLargeRunParams,
		ModelName:         "deepseek-r1-0528",
		Tag:               "0.0.1",
	},
	"deepseek-v3-0324": {
		BaseCommand:       "accelerate launch",
		AccelerateParams:  defaultAccelerateParams,
		InferenceMainFile: defaultTransformersMainFile,
		ModelRunParams:    deepseekLargeRunParams,
		ModelName:         "deepseek-v3-0324",
		Tag:               "0.0.1",
	},
}
