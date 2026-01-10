# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import importlib.util
import os
import sys

import pytest
import yaml

# Load the module dynamically since it has a hyphen in the name
current_dir = os.path.dirname(os.path.abspath(__file__))
module_path = os.path.join(current_dir, "preset_generator.py")
spec = importlib.util.spec_from_file_location("preset_generator", module_path)
preset_generator = importlib.util.module_from_spec(spec)
sys.modules["preset_generator"] = preset_generator
spec.loader.exec_module(preset_generator)

PresetGenerator = preset_generator.PresetGenerator

EXPECTED_OUTPUTS = {
    # outdated, legacy model. model files in .bin .safetensors format
    "tiiuae/falcon-7b-instruct": """attn_type: MQA (Multi-Query Attention)
name: falcon-7b-instruct
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 77Gi
model_file_size_gb: 27
bytes_per_token: 8192
model_token_limit: 2048
vllm:
  model_name: falcon-7b-instruct
  model_run_params:
    load_format: auto
    config_format: auto
    tokenizer_mode: auto
  disallow_lora: false
""",
    # modern standard model with GQA attention.
    "microsoft/Phi-4-mini-instruct": """attn_type: GQA (Grouped-Query Attention)
name: phi-4-mini-instruct
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 58Gi
model_file_size_gb: 8
bytes_per_token: 131072
model_token_limit: 131072
vllm:
  model_name: phi-4-mini-instruct
  model_run_params:
    load_format: auto
    config_format: auto
    tokenizer_mode: auto
  disallow_lora: false
""",
    # modern standard model with MLA attention.
    "deepseek-ai/DeepSeek-R1": """attn_type: MLA (Multi-Latent Attention)
name: deepseek-r1
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 692Gi
model_file_size_gb: 642
bytes_per_token: 70272
model_token_limit: 163840
vllm:
  model_name: deepseek-r1
  model_run_params:
    load_format: auto
    config_format: auto
    tokenizer_mode: auto
  disallow_lora: false
""",
    # model in mistral format with GQA attention.
    "mistralai/Ministral-3-8B-Instruct-2512": """attn_type: GQA (Grouped-Query Attention)
name: ministral-3-8b-instruct-2512
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 60Gi
model_file_size_gb: 10
bytes_per_token: 139264
model_token_limit: 262144
vllm:
  model_name: ministral-3-8b-instruct-2512
  model_run_params:
    load_format: mistral
    config_format: mistral
    tokenizer_mode: mistral
  disallow_lora: false
""",
    # model in mistral format with MLA attention.
    "mistralai/Mistral-Large-3-675B-Instruct-2512": """attn_type: MLA (Multi-Latent Attention)
name: mistral-large-3-675b-instruct-2512
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 685Gi
model_file_size_gb: 635
bytes_per_token: 70272
model_token_limit: 294912
vllm:
  model_name: mistral-large-3-675b-instruct-2512
  model_run_params:
    load_format: mistral
    config_format: mistral
    tokenizer_mode: mistral
  disallow_lora: false
""",
}

def test_get_reasoning_parser():
  """Test get_reasoning_parser function with various model names."""
  # Test exact matches at start of model name
  assert preset_generator.get_reasoning_parser("deepseek-r1") == "deepseek_r1"
  assert preset_generator.get_reasoning_parser("deepseek-r1-distill-qwen-32b") == "deepseek_r1"
  assert preset_generator.get_reasoning_parser("deepseek-v3") == "deepseek_v3"
  assert preset_generator.get_reasoning_parser("deepseek-v3-base") == "deepseek_v3"
  
  # Test other model types
  assert preset_generator.get_reasoning_parser("ernie-4.5") == "ernie45"
  assert preset_generator.get_reasoning_parser("glm-4.5") == "glm45"
  assert preset_generator.get_reasoning_parser("hunyuan-a13b") == "hunyuan_a13b"
  assert preset_generator.get_reasoning_parser("granite-3.2") == "granite"
  assert preset_generator.get_reasoning_parser("minimax-m2") == "minimax_m2_append_think"
  assert preset_generator.get_reasoning_parser("qwen3") == "qwen3"
  assert preset_generator.get_reasoning_parser("qwq-32b") == "deepseek_r1"
  assert preset_generator.get_reasoning_parser("qwq-32b-preview") == "deepseek_r1"
  
  # Test models that don't match any pattern
  assert preset_generator.get_reasoning_parser("phi-4-mini-instruct") == ""
  assert preset_generator.get_reasoning_parser("llama-3.1-8b") == ""
  assert preset_generator.get_reasoning_parser("gpt-4") == ""
  assert preset_generator.get_reasoning_parser("") == ""
  
  # Test case sensitivity (should not match if pattern is different case)
  assert preset_generator.get_reasoning_parser("DeepSeek-R1") == ""
  assert preset_generator.get_reasoning_parser("DEEPSEEK-R1") == ""

def test_get_tool_call_parser():
  """Test get_tool_call_parser function with various model names."""
  # Test special case: deepseek-v3.1 should always return deepseek_v31
  assert preset_generator.get_tool_call_parser("deepseek-v3.1") == "deepseek_v31"
  assert preset_generator.get_tool_call_parser("deepseek-v3.1-base") == "deepseek_v31"

  # Test other deepseek models
  assert preset_generator.get_tool_call_parser("deepseek-r1") == "deepseek_v3"
  assert preset_generator.get_tool_call_parser("deepseek-r1-distill") == "deepseek_v3"
  assert preset_generator.get_tool_call_parser("deepseek-v3") == "deepseek_v3"
  assert preset_generator.get_tool_call_parser("deepseek-v3-base") == "deepseek_v3"

  # Test hermes models
  assert preset_generator.get_tool_call_parser("hermes-2") == "hermes"
  assert preset_generator.get_tool_call_parser("hermes-2-pro") == "hermes"
  assert preset_generator.get_tool_call_parser("hermes-3") == "hermes"
  assert preset_generator.get_tool_call_parser("hermes-3-llama") == "hermes"

  # Test mistral models
  assert preset_generator.get_tool_call_parser("mistral") == "mistral"
  assert preset_generator.get_tool_call_parser("mistral-7b") == "mistral"

  # Test llama models
  assert preset_generator.get_tool_call_parser("meta-llama-3") == "llama3_json"
  assert preset_generator.get_tool_call_parser("meta-llama-3.1-8b") == "llama3_json"
  assert preset_generator.get_tool_call_parser("meta-llama-4") == "llama4_pythonic"
  assert preset_generator.get_tool_call_parser("meta-llama-4-instruct") == "llama4_pythonic"

  # Test granite models
  assert preset_generator.get_tool_call_parser("granite-3") == "granite"
  assert preset_generator.get_tool_call_parser("granite-3.1") == "granite"
  assert preset_generator.get_tool_call_parser("granite-4") == "hermes"
  assert preset_generator.get_tool_call_parser("granite-4-8b") == "hermes"

  # Test other model types
  assert preset_generator.get_tool_call_parser("internlm") == "internlm"
  assert preset_generator.get_tool_call_parser("ai21-jamba") == "jamba"
  assert preset_generator.get_tool_call_parser("qwq-32b") == "hermes"
  assert preset_generator.get_tool_call_parser("qwen2.5") == "hermes"
  assert preset_generator.get_tool_call_parser("minimax") == "minimax"
  assert preset_generator.get_tool_call_parser("kimi_k2") == "kimi_k2"
  assert preset_generator.get_tool_call_parser("hunyuan-a13b") == "hunyuan_a13b"
  assert preset_generator.get_tool_call_parser("longcat") == "longcat"
  assert preset_generator.get_tool_call_parser("glm-4") == "glm45"
  assert preset_generator.get_tool_call_parser("qwen3") == "qwen3_xml"
  assert preset_generator.get_tool_call_parser("olmo-3") == "olmo3"

  # Test models that don't match any pattern
  assert preset_generator.get_tool_call_parser("phi-4-mini-instruct") == ""
  assert preset_generator.get_tool_call_parser("gpt-4") == ""
  assert preset_generator.get_tool_call_parser("") == ""
  assert preset_generator.get_tool_call_parser("random-model") == ""

  # Test case sensitivity (should not match if pattern is different case)
  assert preset_generator.get_tool_call_parser("DeepSeek-V3") == ""
  assert preset_generator.get_tool_call_parser("MISTRAL") == ""