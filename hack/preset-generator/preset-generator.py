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
import os
from dataclasses import asdict, dataclass, field

import yaml
from huggingface_hub import HfFileSystem


@dataclass
class Metadata:
    name: str = ""
    type: str = "tfs"
    version: str = "0.0.1"
    download_at_runtime: bool = (
        True  # Typically true for Kaito presets generated dynamically
    )
    download_auth_required: bool = False
    tag: str | None = None


@dataclass
class VLLMParam:
    # TODO: figure out tool-parser, reasoning-parser, model weights format, etc.
    model_name: str = ""
    model_run_params: dict[str, str] = field(default_factory=dict)
    disallow_lora: bool = False


@dataclass
class RuntimeParam:
    vllm: VLLMParam = field(default_factory=VLLMParam)


@dataclass
class PresetParam(Metadata, RuntimeParam):
    disk_storage_requirement: str = "30Gi"  # Default, to be calculated
    image_access_mode: str = "public"
    total_safe_tensor_file_size: str = ""
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

    def calculate_storage(self):
        logging.info(f"Calculating storage requirements for {self.model_repo}...")

        # Check if private/gated by attempting to list
        try:
            # Just a check to see if we can access
            self.fs.ls(self.model_repo)
        except Exception as e:
            if "401" in str(e) or "403" in str(e):
                logging.error(f"Unauthorized. Token might be required. {e}")
                self.param.download_auth_required = True
                self.param.image_access_mode = "private"
                return
            logging.warning(f"Error accessing model: {e}")

        # List files with details
        files_info = self.fs.ls(self.model_repo, detail=True)

        # Filter for safetensors or bin
        safetensors = [f for f in files_info if f["name"].endswith(".safetensors")]
        bin_files = [f for f in files_info if f["name"].endswith(".bin")]

        selected_files = safetensors if safetensors else bin_files

        if not selected_files:
            logging.info("No .safetensors or .bin files found.")

        total_bytes = sum(f["size"] for f in selected_files)

        logging.info(f"Found {len(selected_files)} model files.")

        total_gib = total_bytes / (1024**3)
        self.param.total_safe_tensor_file_size = f"{total_gib:.2f}Gi"

        # Estimate disk storage requirement (usually model size * 1.5 rounded up)
        disk_req_gib = int(total_gib * 1.5) + 1
        self.param.disk_storage_requirement = f"{disk_req_gib}Gi"
        logging.info(f"Total size: {total_gib:.2f} GiB")

    def calculate_compute_params(self):
        logging.info("Fetching config for compute params...")
        config_path = f"{self.model_repo}/config.json"
        try:
            with self.fs.open(config_path, "r") as f:
                config = json.load(f)

            hidden_layers = config.get("num_hidden_layers", 0)
            hidden_size = config.get("hidden_size", 0)
            attention_heads = config.get("num_attention_heads", 0)

            # KV heads
            if "num_key_value_heads" in config:
                kv_heads = config["num_key_value_heads"]
            elif config.get("multi_query", False):
                kv_heads = 1
            else:
                kv_heads = attention_heads

            if hidden_layers and hidden_size and attention_heads:
                # Formula: 2 * num_layers * num_kv_heads * (hidden_size / num_attention_heads) * 2 bytes
                bytes_per_token = (
                    2 * hidden_layers * kv_heads * (hidden_size / attention_heads) * 2
                )
                self.param.bytes_per_token = int(bytes_per_token)

            # Max context window
            max_pos = (
                config.get("max_position_embeddings")
                or config.get("n_ctx")
                or config.get("seq_length")
                or config.get("max_seq_len")
                or config.get("max_sequence_length")
            )
            if max_pos:
                self.param.model_token_limit = int(max_pos)

        except Exception as e:
            logging.warning(f"Failed to calculate compute params: {e}")

    def generate(self) -> str:
        self.calculate_storage()
        self.calculate_compute_params()

        data = asdict(self.param)
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
