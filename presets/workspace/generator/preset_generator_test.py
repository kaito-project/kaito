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


@pytest.mark.parametrize(
    "model_name",
    [
        "microsoft/Phi-4-mini-instruct",
        "tiiuae/falcon-7b-instruct",
        "mistralai/Ministral-3-8B-Instruct-2512",
        "mistralai/Mistral-Large-3-675B-Instruct-2512",
        "deepseek-ai/DeepSeek-R1",
    ],
)
def test_preset_generator(model_name):
    generator = PresetGenerator(model_name)
    output = generator.generate()

    expected = EXPECTED_OUTPUTS[model_name]

    # Compare parsed YAML to avoid formatting differences
    assert yaml.safe_load(output) == yaml.safe_load(expected)


def test_get_reasoning_parser():
    """Test get_reasoning_parser function with various model names."""
    # Test exact matches at start of model name
    assert preset_generator.get_reasoning_parser("deepseek-r1") == "deepseek_r1"
    assert (
        preset_generator.get_reasoning_parser("deepseek-r1-distill-qwen-32b")
        == "deepseek_r1"
    )
    assert preset_generator.get_reasoning_parser("deepseek-v3") == "deepseek_v3"
    assert preset_generator.get_reasoning_parser("deepseek-v3-base") == "deepseek_v3"

    # Test other model types
    assert preset_generator.get_reasoning_parser("ernie-4.5") == "ernie45"
    assert preset_generator.get_reasoning_parser("glm-4.5") == "glm45"
    assert preset_generator.get_reasoning_parser("hunyuan-a13b") == "hunyuan_a13b"
    assert preset_generator.get_reasoning_parser("granite-3.2") == "granite"
    assert (
        preset_generator.get_reasoning_parser("minimax-m2") == "minimax_m2_append_think"
    )
    assert preset_generator.get_reasoning_parser("qwen3") == "qwen3"
    assert preset_generator.get_reasoning_parser("qwq-32b") == "deepseek_r1"
    assert preset_generator.get_reasoning_parser("qwq-32b-preview") == "deepseek_r1"

    # Test models that don't match any pattern
    assert preset_generator.get_reasoning_parser("phi-4-mini-instruct") == ""
    assert preset_generator.get_reasoning_parser("llama-3.1-8b") == ""
    assert preset_generator.get_reasoning_parser("gpt-4") == ""
    assert preset_generator.get_reasoning_parser("") == ""

    # Test case insensitivity (should match regardless of case)
    assert preset_generator.get_reasoning_parser("DeepSeek-R1") == "deepseek_r1"
    assert preset_generator.get_reasoning_parser("DEEPSEEK-R1") == "deepseek_r1"


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
    assert (
        preset_generator.get_tool_call_parser("meta-llama-4-instruct")
        == "llama4_pythonic"
    )

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
    assert preset_generator.get_tool_call_parser("qwen3") == "hermes"
    assert preset_generator.get_tool_call_parser("olmo-3") == "olmo3"

    # Test models that don't match any pattern
    assert preset_generator.get_tool_call_parser("phi-4-mini-instruct") == ""
    assert preset_generator.get_tool_call_parser("gpt-4") == ""
    assert preset_generator.get_tool_call_parser("") == ""
    assert preset_generator.get_tool_call_parser("random-model") == ""

    # Test case insensitivity (should match regardless of case)
    assert preset_generator.get_tool_call_parser("DeepSeek-V3") == "deepseek_v3"
    assert preset_generator.get_tool_call_parser("MISTRAL") == "mistral"


@pytest.fixture
def mock_vllm_response():
    """Mock response content from vLLM supported models documentation."""
    return """
# Supported Models

| Architecture | Model Family | Example Models | ... | ... |
|--------------|--------------|----------------|-----|-----|
| `LlamaForCausalLM` | LLaMA | `meta-llama/Llama-2-7b-hf`, `meta-llama/Llama-3-8b`, etc. | .. | .. |
| `QWenLMHeadModel` | Qwen | `Qwen/Qwen-7B`, `Qwen/Qwen-7B-Chat`, etc. | .. | .. |
| `Qwen2ForCausalLM` | QwQ, Qwen2 | `Qwen/QwQ-32B-Preview`, `Qwen/Qwen2-7B-Instruct`, `Qwen/Qwen2-7B`, etc. | .. | .. |
| `MistralForCausalLM` | Mistral | `mistralai/Mistral-7B-v0.1` | .. | .. |
| `PhiForCausalLM` | Phi | `microsoft/phi-2` | .. | .. |

Some text without proper format
| Invalid | Row | NoModels | .. |

| With Single Column Only |

| `FalconForCausalLM` | Falcon | `tiiuae/falcon-7b`, `tiiuae/falcon-40b` | .. | .. |
| `GPT2LMHeadModel` | GPT-2 | `gpt2`, `gpt2-medium` | .. | .. |
| `DeepseekV2ForCausalLM` | DeepSeek-V2 | `deepseek-ai/DeepSeek-V2`, `deepseek-ai/DeepSeek-V2-Lite` | .. | .. |

Models without owner format should be filtered:
| `SomeModel` | Test | `standalone-model`, `owner/valid-model` | .. | .. |
"""


def test_get_all_vllm_models_basic(monkeypatch, mock_vllm_response):
    """Test get_all_vllm_models returns correctly parsed and filtered models."""

    class MockResponse:
        def __init__(self, text):
            self.text = text

        def raise_for_status(self):
            pass

    def mock_get(url):
        return MockResponse(mock_vllm_response)

    monkeypatch.setattr(preset_generator.requests, "get", mock_get)

    result = preset_generator.get_all_vllm_models()

    # Should be sorted list
    assert isinstance(result, list)

    # Verify expected models are included
    assert "meta-llama/Llama-2-7b-hf" in result
    assert "meta-llama/Llama-3-8b" in result
    assert "Qwen/Qwen-7B" in result
    assert "Qwen/Qwen-7B-Chat" in result
    assert "Qwen/QwQ-32B-Preview" in result
    assert "Qwen/Qwen2-7B-Instruct" in result
    assert "Qwen/Qwen2-7B" in result
    assert "mistralai/Mistral-7B-v0.1" in result
    assert "microsoft/phi-2" in result
    assert "tiiuae/falcon-7b" in result
    assert "tiiuae/falcon-40b" in result
    assert "deepseek-ai/DeepSeek-V2" in result
    assert "deepseek-ai/DeepSeek-V2-Lite" in result
    assert "owner/valid-model" in result

    # Models without owner/model format should be filtered out
    assert "gpt2" not in result
    assert "gpt2-medium" not in result
    assert "standalone-model" not in result

    # Verify it's sorted
    assert result == sorted(result)


def test_get_all_vllm_models_empty_response(monkeypatch):
    """Test get_all_vllm_models with empty response."""

    class MockResponse:
        def __init__(self):
            self.text = ""

        def raise_for_status(self):
            pass

    def mock_get(url):
        return MockResponse()

    monkeypatch.setattr(preset_generator.requests, "get", mock_get)

    result = preset_generator.get_all_vllm_models()
    assert result == []


def test_get_all_vllm_models_http_error(monkeypatch):
    """Test get_all_vllm_models handles HTTP errors."""

    def mock_get(url):
        raise preset_generator.requests.exceptions.HTTPError("404 Not Found")

    monkeypatch.setattr(preset_generator.requests, "get", mock_get)

    with pytest.raises(preset_generator.requests.exceptions.HTTPError):
        preset_generator.get_all_vllm_models()


def test_get_all_vllm_models_no_duplicates(monkeypatch):
    """Test that duplicate models are handled correctly."""

    duplicate_response = """
| `LlamaForCausalLM` | LLaMA | `meta-llama/Llama-2-7b-hf`, `meta-llama/Llama-2-7b-hf`, `owner/model` | .. | .. |
| `QWenLMHeadModel` | Qwen | `owner/model`, `Qwen/Qwen-7B` | .. | .. |
"""

    class MockResponse:
        def __init__(self, text):
            self.text = text

        def raise_for_status(self):
            pass

    def mock_get(url):
        return MockResponse(duplicate_response)

    monkeypatch.setattr(preset_generator.requests, "get", mock_get)

    result = preset_generator.get_all_vllm_models()

    # Each model should appear only once
    assert result.count("meta-llama/Llama-2-7b-hf") == 1
    assert result.count("owner/model") == 1
    assert result.count("Qwen/Qwen-7B") == 1
