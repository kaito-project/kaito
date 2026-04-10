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

// Shared vLLM ModelRunParams for model families that use the same parameters.
var (
	deepseekLlama8bRunParamsVLLM = map[string]string{
		"reasoning-parser": "deepseek_r1",
	}
	deepseekQwen14bRunParamsVLLM = map[string]string{
		"reasoning-parser": "deepseek_r1",
	}
	deepseekR1RunParamsVLLM = map[string]string{
		"reasoning-parser":        "deepseek_r1",
		"chat-template":           "/workspace/chat_templates/tool-chat-deepseekr1.jinja",
		"tool-call-parser":        "deepseek_v3",
		"enable-auto-tool-choice": "",
	}
	deepseekV3RunParamsVLLM = map[string]string{
		"chat-template":           "/workspace/chat_templates/tool-chat-deepseekv3.jinja",
		"tool-call-parser":        "deepseek_v3",
		"enable-auto-tool-choice": "",
	}
	falconRunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/falcon-instruct.jinja",
	}
	gemma3RunParamsVLLM = map[string]string{}
	gptRunParamsVLLM    = map[string]string{} // TODO: add the dtype to the gpt model
	llamaRunParamsVLLM  = map[string]string{
		"chat-template":    "/workspace/chat_templates/tool-chat-llama3.1-json.jinja",
		"tool-call-parser": "llama3_json",
		// pin the attention backend to triton for llama3 models, as flashinfer is unavailable in KAITO base image.
		"attention-backend":       "TRITON_ATTN",
		"enable-auto-tool-choice": "",
	}
	mistralRunParamsVLLM = map[string]string{
		"tool-call-parser":        "mistral",
		"enable-auto-tool-choice": "",
	}
	mistral3RunParamsVLLM = map[string]string{
		"tool-call-parser":        "mistral",
		"tokenizer_mode":          "mistral",
		"config_format":           "mistral",
		"load_format":             "mistral",
		"enable-auto-tool-choice": "",
	}
	phiRunParamsVLLM      = map[string]string{}
	phi4MiniRunParamsVLLM = map[string]string{
		"chat-template":           "/workspace/chat_templates/tool-chat-phi4-mini.jinja",
		"tool-call-parser":        "phi4_mini_json",
		"enable-auto-tool-choice": "",
	}
	qwenRunParamsVLLM = map[string]string{
		"chat-template":           "/workspace/chat_templates/tool-chat-hermes.jinja",
		"tool-call-parser":        "hermes",
		"enable-auto-tool-choice": "",
	}
)

// VLLMInferenceParameters maps preset model names to their vLLM runtime
// parameters for inference. Each model's GetInferenceParameters() should
// look up its VLLM config from this map instead of hardcoding the values inline.
var VLLMInferenceParameters = map[string]model.VLLMParam{
	// DeepSeek family
	"deepseek-r1-distill-llama-8b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "deepseek-r1-distill-llama-8b",
		ModelRunParams: deepseekLlama8bRunParamsVLLM,
	},
	"deepseek-r1-distill-qwen-14b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "deepseek-r1-distill-qwen-14b",
		ModelRunParams: deepseekQwen14bRunParamsVLLM,
	},
	"deepseek-r1-0528": {
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            "deepseek-r1-0528",
		ModelRunParams:       deepseekR1RunParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	},
	"deepseek-v3-0324": {
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            "deepseek-v3-0324",
		ModelRunParams:       deepseekV3RunParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	},

	// Falcon family
	"falcon-7b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "falcon-7b",
		ModelRunParams: falconRunParamsVLLM,
		DisallowLoRA:   true,
	},
	"falcon-7b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "falcon-7b-instruct",
		ModelRunParams: falconRunParamsVLLM,
		DisallowLoRA:   true,
	},
	"falcon-40b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "falcon-40b",
		ModelRunParams: falconRunParamsVLLM,
	},
	"falcon-40b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "falcon-40b-instruct",
		ModelRunParams: falconRunParamsVLLM,
	},

	// Gemma-3 family
	"gemma-3-4b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "gemma-3-4b-instruct",
		ModelRunParams: gemma3RunParamsVLLM,
	},
	"gemma-3-27b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "gemma-3-27b-instruct",
		ModelRunParams: gemma3RunParamsVLLM,
	},

	// GPT-OSS family
	"gpt-oss-20b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "gpt-oss-20b",
		ModelRunParams: gptRunParamsVLLM,
	},
	"gpt-oss-120b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "gpt-oss-120b",
		ModelRunParams: gptRunParamsVLLM,
	},

	// Llama-3 family
	"llama-3.1-8b-instruct": {
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            "llama-3.1-8b-instruct",
		ModelRunParams:       llamaRunParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	},
	"llama-3.3-70b-instruct": {
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            "llama-3.3-70b-instruct",
		ModelRunParams:       llamaRunParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	},

	// Mistral family
	"mistral-7b": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "mistral-7b",
		ModelRunParams: mistralRunParamsVLLM,
	},
	"mistral-7b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "mistral-7b-instruct",
		ModelRunParams: mistralRunParamsVLLM,
	},
	"ministral-3-3b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "ministral-3-3b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"ministral-3-8b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "ministral-3-8b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"ministral-3-14b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "ministral-3-14b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"mistral-large-3-675b-instruct": {
		BaseCommand:          DefaultVLLMCommand,
		ModelName:            "mistral-large-3-675b-instruct",
		ModelRunParams:       mistral3RunParamsVLLM,
		RayLeaderBaseCommand: DefaultVLLMRayLeaderBaseCommand,
		RayWorkerBaseCommand: DefaultVLLMRayWorkerBaseCommand,
	},

	// Phi-3 family
	"phi-3-mini-4k-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-3-mini-4k-instruct",
		ModelRunParams: phiRunParamsVLLM,
	},
	"phi-3-mini-128k-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-3-mini-128k-instruct",
		ModelRunParams: phiRunParamsVLLM,
	},
	"phi-3.5-mini-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-3.5-mini-instruct",
		ModelRunParams: phiRunParamsVLLM,
	},
	"phi-3-medium-4k-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-3-medium-4k-instruct",
		ModelRunParams: phiRunParamsVLLM,
	},
	"phi-3-medium-128k-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-3-medium-128k-instruct",
		ModelRunParams: phiRunParamsVLLM,
	},

	// Phi-4 family
	"phi-4": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-4",
		ModelRunParams: phiRunParamsVLLM,
	},
	"phi-4-mini-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "phi-4-mini-instruct",
		ModelRunParams: phi4MiniRunParamsVLLM,
	},

	// Qwen family
	"qwen2.5-coder-7b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "qwen2.5-coder-7b-instruct",
		ModelRunParams: qwenRunParamsVLLM,
	},
	"qwen2.5-coder-32b-instruct": {
		BaseCommand:    DefaultVLLMCommand,
		ModelName:      "qwen2.5-coder-32b-instruct",
		ModelRunParams: qwenRunParamsVLLM,
	},
}
