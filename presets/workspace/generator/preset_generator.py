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

import argparse
import json
import logging
import math
import os
import re
from dataclasses import asdict, dataclass, field
from typing import Any

import yaml
from huggingface_hub import HfFileSystem
from huggingface_hub.utils import GatedRepoError, RepositoryNotFoundError

SYSTEM_FILE_DISKSIZE_GIB = 50
DEFAULT_MODEL_TOKEN_LIMIT = 2048


def filter_list_by_regex(
    input_list: list[dict], allow_pattern: list[str]
) -> list[dict]:
    if not allow_pattern:
        return input_list
    filtered_list = []
    for item in input_list:
        matched = False
        for pattern in allow_pattern:
            if re.search(pattern, item["name"]):
                matched = True
                break
        if matched:
            filtered_list.append(item)
    return filtered_list


def get_config_attr(config: Any, attributes: list, default: Any = None) -> Any:
    """Helper to safely fetch attributes handling different naming conventions."""
    for attr in attributes:
        if isinstance(config, dict):
            if attr in config:
                return config[attr]
        elif hasattr(config, attr):
            return getattr(config, attr)
    return default


@dataclass
class Metadata:
    name: str = ""
    type: str = "tfs"
    version: str = "0.0.1"
    download_at_runtime: bool = (
        True  # Typically true for Kaito presets generated dynamically
    )
    download_auth_required: bool = False


@dataclass
class VLLMParam:
    # TODO: figure out tool-parser, reasoning-parser. etc.
    model_name: str = ""
    model_run_params: dict[str, str] = field(default_factory=dict)
    disallow_lora: bool = False


@dataclass
class RuntimeParam:
    vllm: VLLMParam = field(default_factory=VLLMParam)
    attn_type: str = "MHA"  # Default, to be determined


@dataclass
class PresetParam(Metadata, RuntimeParam):
    disk_storage_requirement: str = "50Gi"  # Default, to be calculated
    model_file_size_gb: float = 0.0
    # TODO: adapt for different attention architecture
    bytes_per_token: int = 0
    model_token_limit: int = 0


class PresetGenerator:
    def __init__(self, model_repo: str, token: str | None = None):
        self.model_repo = model_repo
        self.token = token or os.environ.get("HF_TOKEN")
        self.fs = HfFileSystem(token=self.token)
        # Initialize default PresetParam
        model_name_safe = model_repo.split("/")[-1].lower()
        self.param = PresetParam(
            name=model_name_safe,
            type="tfs",
            version="0.0.1",
        )

        # params figured out during analysis
        self.load_format = "auto"
        self.config_format = "auto"
        self.tokenizer_mode = "auto"
        self.config = None

    def fetch_model_metadata(self):
        logging.info(f"Fetching metadata for {self.model_repo}...")

        files_info = []
        try:
            # we can list files only if repo exists.
            # notes: private repo without token will still list files, but accessing them will fail.
            files_info = self.fs.ls(self.model_repo, detail=True)
        except (RepositoryNotFoundError, FileNotFoundError) as e:
            logging.fatal(f"Model repository not found: {e}")
            return
        except Exception as e:
            logging.fatal(f"Error accessing model: {e}")
            return

        # Filter for safetensors, bin
        safetensors = filter_list_by_regex(files_info, [r".*\.safetensors", r".*\.bin"])
        mistral_files = filter_list_by_regex(
            files_info, [r"consolidated.*\.safetensors"]
        )

        # follow the vllm logic to detect model format
        # ref: https://github.com/vllm-project/vllm/blob/v0.12.0/vllm/model_executor/model_loader/default_loader.py#L118
        if mistral_files:
            self.load_format = "mistral"
            self.config_format = "mistral"
            self.tokenizer_mode = "mistral"
            config_file = "params.json"
            selected_files = mistral_files
        elif safetensors:
            config_file = "config.json"
            selected_files = safetensors
        else:
            selected_files = []

        if not selected_files:
            logging.fatal("No .safetensors or .bin files found.")

        logging.info(f"Found {len(selected_files)} model files.")
        total_bytes = sum(f["size"] for f in selected_files)
        self.param.model_file_size_gb = math.ceil(total_bytes / (1024**3))

        logging.info("Fetching config for compute params...")
        config_path = f"{self.model_repo}/{config_file}"
        try:
            # Try without token first
            with HfFileSystem().open(config_path, "r") as f:
                self.config = json.load(f)
        except (GatedRepoError, PermissionError) as e:
            logging.info(f"Unauthorized with no token. Token is required. {e}")
            self.param.download_auth_required = True

            # Try again with token
            try:
                with self.fs.open(config_path, "r") as f:
                    self.config = json.load(f)
            except Exception as e_auth:
                logging.fatal(f"Failed to access model with token: {e_auth}")
                return
        except Exception as e:
            logging.fatal(f"Error accessing model: {e}")
            return
        logging.info(f"Model Config is {self.config}")

    def parse_model_metadata(self):
        # Max context window
        max_pos = get_config_attr(
            self.config,
            [
                "max_position_embeddings",
                "n_ctx",
                "seq_length",
                "max_seq_len",
                "max_sequence_length",
            ],
            DEFAULT_MODEL_TOKEN_LIMIT,
        )
        self.param.model_token_limit = int(max_pos)

        # TODO: parse quantization config

    @staticmethod
    def calculate_storage_size(model_file_size_gib: float) -> int:
        disk_req_gib = int(model_file_size_gib + SYSTEM_FILE_DISKSIZE_GIB)
        return disk_req_gib

    @staticmethod
    def calculate_kvcache_token_size(config: dict) -> tuple[int, str]:
        # --- 1. Extract Dimensions ---
        # config name comes from
        # - common configuration: https://huggingface.co/docs/transformers/v4.57.3/en/main_classes/configuration
        # - non-standard, custom configuration: https://github.com/vllm-project/vllm/tree/v0.12.0/vllm/model_executor/models
        hidden_size = get_config_attr(config, ["hidden_size", "n_embd", "d_model"])
        hidden_layers = get_config_attr(
            config, ["num_hidden_layers", "n_layer", "n_layers"], default=0
        )
        attention_heads = get_config_attr(
            config, ["num_attention_heads", "n_head", "n_heads"]
        )
        kv_heads = get_config_attr(
            config, ["num_key_value_heads", "n_head_kv", "n_kv_heads"]
        )
        head_dim = get_config_attr(config, ["head_dim"])

        # Calculate head_dim if missing (Standard Transformer logic)
        if head_dim is None and hidden_size and attention_heads:
            head_dim = hidden_size // attention_heads

        # DeepSeek MLA Specifics
        kv_lora_rank = config.get("kv_lora_rank")
        qk_rope_head_dim = config.get("qk_rope_head_dim", 0)

        # Fallback for kv_heads (Default to MHA)
        if kv_heads is None and attention_heads:
            if get_config_attr(config, ["multi_query"]):
                kv_heads = 1
            else:
                kv_heads = attention_heads

        # --- 2. Determine Architecture & Cache Size ---
        # consider the attn arch supported by vllm. (MHA, MQA, GQA, MLA)
        # ref: https://github.com/vllm-project/vllm/blob/v0.12.0/vllm/attention/layer.py#L161
        attn_type = "Unknown"
        elements_per_token = 0

        # CASE A: Multi-Latent Attention (MLA) - DeepSeek V2/V3
        if kv_lora_rank is not None:
            attn_type = "MLA"
            # MLA Cache = Compressed Latent Vector + Decoupled RoPE Key
            # DeepSeek V2/V3 caches a SINGLE latent vector + RoPE part per token (shared across heads)
            elements_per_token = kv_lora_rank + qk_rope_head_dim
            # Note: No factor of 2 (K+V) because they are compressed into one vector

        # CASE B: Standard MHA/GQA/MQA
        elif attention_heads and kv_heads and head_dim:
            # Standard Cache = 2 (Key + Value) * Num_KV_Heads * Head_Dim
            elements_per_token = 2 * kv_heads * head_dim

            if attention_heads == kv_heads:
                attn_type = "MHA (Multi-Head Attention)"
            elif kv_heads == 1:
                attn_type = "MQA"
            else:
                attn_type = "GQA"

        # --- 3. Calculate Memory Usage ---
        # Total params per token (across all layers)
        total_elements = elements_per_token * hidden_layers
        # TODO: honor quantization
        token_size = total_elements * 2  # 2 bytes per element (fp16)

        return token_size, attn_type

    def finalize_params(self):
        # Disk storage requirement
        self.param.disk_storage_requirement = (
            f"{self.calculate_storage_size(self.param.model_file_size_gb)}Gi"
        )

        # VLLM params
        self.param.vllm.model_name = self.param.name
        self.param.vllm.model_run_params = {
            "load_format": self.load_format,
            "config_format": self.config_format,
            "tokenizer_mode": self.tokenizer_mode,
        }

        # KV cache bytes per token
        bpt, attn_type = self.calculate_kvcache_token_size(self.config)
        self.param.bytes_per_token = bpt
        self.param.attn_type = attn_type

    def generate(self) -> str:
        self.fetch_model_metadata()
        self.parse_model_metadata()
        self.finalize_params()

        data = asdict(self.param)
        # Ensure runtime parameters (vllm) are at the end of the YAML
        if "vllm" in data:
            data["vllm"] = data.pop("vllm")
        return yaml.dump(data, sort_keys=False, default_flow_style=False)


def main():
    parser = argparse.ArgumentParser(description="Generate Kaito Preset YAML")
    parser.add_argument(
        "model_repo",
        help="Hugging Face model repository (e.g., microsoft/Phi-4-mini-instruct)",
    )
    parser.add_argument("--token", help="Hugging Face API token", default=None)
    parser.add_argument("--debug", action="store_true", help="Enable debug logging")
    args = parser.parse_args()

    # Configure logging
    log_level = logging.INFO if args.debug else logging.WARNING
    logging.basicConfig(level=log_level, format="[%(levelname)s] %(message)s")

    generator = PresetGenerator(args.model_repo, args.token)
    yaml_output = generator.generate()
    logging.info("--- Generated Preset YAML ---\n")
    print(yaml_output)


if __name__ == "__main__":
    main()
