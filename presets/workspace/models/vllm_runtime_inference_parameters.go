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
	deepseekR1RunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/tool-chat-deepseekr1.jinja",
	}
	deepseekV3RunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/tool-chat-deepseekv3.jinja",
	}
	falconRunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/falcon-instruct.jinja",
	}
	llamaRunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/tool-chat-llama3.1-json.jinja",
		// pin the attention backend to triton for llama3 models, as flashinfer is unavailable in KAITO base image.
		"attention-backend": "TRITON_ATTN",
	}
	mistralRunParamsVLLM = map[string]string{
		"tokenizer_mode": "mistral",
		"config_format":  "mistral",
		"load_format":    "mistral",
	}
	mistral3RunParamsVLLM = map[string]string{
		"tokenizer_mode": "mistral",
		"config_format":  "mistral",
		"load_format":    "mistral",
	}
	phi4MiniRunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/tool-chat-phi4-mini.jinja",
	}
	qwenRunParamsVLLM = map[string]string{
		"chat-template": "/workspace/chat_templates/tool-chat-hermes.jinja",
	}
)

// VLLMInferenceParameters maps preset model names to their vLLM runtime
// parameters for inference. Each model's GetInferenceParameters() should
// look up its VLLM config from this map instead of hardcoding the values inline.
var VLLMInferenceParameters = map[string]model.VLLMParam{
	// DeepSeek family
	"deepseek-r1-0528": {
		ModelRunParams: deepseekR1RunParamsVLLM,
	},
	"deepseek-v3-0324": {
		ModelRunParams: deepseekV3RunParamsVLLM,
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
	"gemma-3-4b-it": {
		ModelName: "gemma-3-4b-instruct",
	},
	"gemma-3-27b-it": {
		ModelName: "gemma-3-27b-instruct",
	},

	// Llama-3 family
	"llama-3.1-8b-instruct": {
		ModelRunParams: llamaRunParamsVLLM,
	},
	"llama-3.3-70b-instruct": {
		ModelRunParams: llamaRunParamsVLLM,
	},

	// Mistral family
	"mistral-7b-v0.3": {
		ModelName:      "mistral-7b",
		ModelRunParams: mistralRunParamsVLLM,
	},
	"mistral-7b-instruct-v0.3": {
		ModelName:      "mistral-7b-instruct",
		ModelRunParams: mistralRunParamsVLLM,
	},
	"ministral-3-3b-instruct-2512": {
		ModelName:      "ministral-3-3b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"ministral-3-8b-instruct-2512": {
		ModelName:      "ministral-3-8b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"ministral-3-14b-instruct-2512": {
		ModelName:      "ministral-3-14b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},
	"mistral-large-3-675b-instruct-2512": {
		ModelName:      "mistral-large-3-675b-instruct",
		ModelRunParams: mistral3RunParamsVLLM,
	},

	// Phi-4 family
	"phi-4-mini-instruct": {
		ModelRunParams: phi4MiniRunParamsVLLM,
	},

	// Qwen family
	"qwen2.5-coder-7b-instruct": {
		ModelRunParams: qwenRunParamsVLLM,
	},
	"qwen2.5-coder-32b-instruct": {
		ModelRunParams: qwenRunParamsVLLM,
	},
}
