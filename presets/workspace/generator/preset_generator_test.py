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
    "tiiuae/falcon-7b-instruct": """attn_type: MQA
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
    "microsoft/Phi-4-mini-instruct": """attn_type: GQA
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
    "deepseek-ai/DeepSeek-R1": """attn_type: MLA
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
    "mistralai/Ministral-3-8B-Instruct-2512": """attn_type: GQA
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
    "mistralai/Mistral-Large-3-675B-Instruct-2512": """attn_type: MLA
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
