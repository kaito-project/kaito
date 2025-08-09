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

import os
from dataclasses import dataclass, field
from typing import Any

import torch
from peft import LoraConfig
from transformers import (
    BitsAndBytesConfig,
    DataCollatorForLanguageModeling,
    PreTrainedTokenizer,
)

target_modules_env = os.environ.get("DEFAULT_TARGET_MODULES", None)
DEFAULT_TARGET_MODULES = (
    [module.strip() for module in target_modules_env.split(",")]
    if target_modules_env
    else None
)

# Consider Future Support for other trainers
# class TrainerTypes(Enum):
# TRAINER = "Trainer"
# SFT_TRAINER = "SFTTrainer"
# DPO_TRAINER = "DPOTrainer"
# REWARD_TRAINER = "RewardTrainer"
# PPO_TRAINER = "PPOTrainer"
# CPO_TRAINER = "CPOTrainer"
# ORPO_TRAINER = "ORPOTrainer"

# @dataclass
# class TrainerType:
#     trainer_type: TrainerTypes = field(default=TrainerTypes.SFT_TRAINER, metadata={"help": "Type of trainer to use for fine-tuning."})


@dataclass
class ExtDataCollator(DataCollatorForLanguageModeling):
    tokenizer: PreTrainedTokenizer | None = field(
        default=PreTrainedTokenizer,
        metadata={"help": "Tokenizer for DataCollatorForLanguageModeling"},
    )


@dataclass
class ExtLoraConfig(LoraConfig):
    """
    Lora Config
    """

    init_lora_weights: bool = field(
        default=True, metadata={"help": "Enable initialization of LoRA weights"}
    )
    target_modules: list[str] | None = field(
        default_factory=lambda: DEFAULT_TARGET_MODULES
        if DEFAULT_TARGET_MODULES
        else None,
        metadata={"help": "List of module names to replace with LoRA."},
    )
    layers_to_transform: list[int] | None = field(
        default=None, metadata={"help": "Layer indices to apply LoRA"}
    )
    layers_pattern: list[str] | None = field(
        default=None, metadata={"help": "Pattern to match layers for LoRA"}
    )
    loftq_config: dict[str, any] = field(
        default_factory=dict, metadata={"help": "LoftQ configuration for quantization"}
    )


@dataclass
class DatasetConfig:
    """
    Config for Dataset
    """

    dataset_path: str | None = field(
        default=None,
        metadata={
            "help": "Where dataset file is located in the /data folder. This path will be appended to /data. This path should be the absolute path in the image or host."
        },
    )
    dataset_extension: str | None = field(
        default=None,
        metadata={
            "help": "Optional explicit file extension of the dataset. If not provided, the extension will be derived from the dataset_path."
        },
    )
    shuffle_dataset: bool = field(
        default=True, metadata={"help": "Whether to shuffle dataset"}
    )
    shuffle_seed: int = field(default=42, metadata={"help": "Seed for shuffling data"})
    # instruction_column: Optional[str] = field(default=None, metadata={"help": "Optional column for detailed instructions, used in more structured tasks like Alpaca-style setups."}) # Consider including in V2
    context_column: str | None = field(
        default=None,
        metadata={
            "help": "Column for additional context or prompts, used for generating responses based on scenarios."
        },
    )
    response_column: str = field(
        default="text",
        metadata={
            "help": "Main text column for standalone entries or the response part in prompt-response setups."
        },
    )
    messages_column: str | None = field(
        default=None,
        metadata={
            "help": "Column containing structured conversational data in JSON format with roles and content, used for chatbot training."
        },
    )
    train_test_split: float = field(
        default=0.8,
        metadata={
            "help": "Split between test and training data (e.g. 0.8 means 80/20% train/test split)"
        },
    )


@dataclass
class ModelConfig:
    """
    Transformers Model Configuration Parameters
    """

    pretrained_model_name_or_path: str | None = field(
        default="/workspace/tfs/weights",
        metadata={
            "help": "Path to the pretrained model or model identifier from huggingface.co/models"
        },
    )
    state_dict: dict[str, Any] | None = field(
        default=None, metadata={"help": "State dictionary for the model"}
    )
    cache_dir: str | None = field(
        default=None, metadata={"help": "Cache directory for the model"}
    )
    from_tf: bool = field(
        default=False, metadata={"help": "Load model from a TensorFlow checkpoint"}
    )
    force_download: bool = field(
        default=False, metadata={"help": "Force the download of the model"}
    )
    resume_download: bool = field(
        default=False, metadata={"help": "Resume an interrupted download"}
    )
    proxies: str | None = field(
        default=None, metadata={"help": "Proxy configuration for downloading the model"}
    )
    output_loading_info: bool = field(
        default=False, metadata={"help": "Output additional loading information"}
    )
    local_files_only: bool = field(
        default=False,
        metadata={"help": "Allow using remote files, default is local only"},
    )
    revision: str = field(
        default="main", metadata={"help": "Specific model version to use"}
    )
    trust_remote_code: bool = field(
        default=False,
        metadata={"help": "Enable trusting remote code when loading the model"},
    )
    load_in_4bit: bool = field(
        default=False, metadata={"help": "Load model in 4-bit mode"}
    )
    load_in_8bit: bool = field(
        default=False, metadata={"help": "Load model in 8-bit mode"}
    )
    torch_dtype: str | None = field(
        default=None, metadata={"help": "The torch dtype for the pre-trained model"}
    )
    device_map: str = field(
        default="auto", metadata={"help": "The device map for the pre-trained model"}
    )
    chat_template: str | None = field(
        default=None,
        metadata={
            "help": "The file path to the chat template, or the template in single-line form for the specified model"
        },
    )

    def __post_init__(self):
        """
        Post-initialization to validate some ModelConfig values
        """
        if self.torch_dtype:
            if isinstance(self.torch_dtype, str) and hasattr(torch, self.torch_dtype):
                self.torch_dtype = getattr(torch, self.torch_dtype)
            elif not isinstance(self.torch_dtype, torch.dtype):
                raise ValueError(f"Invalid torch dtype: {self.torch_dtype}")

    def get_tokenizer_args(self):
        return {
            k: v
            for k, v in self.__dict__.items()
            if k not in ["torch_dtype", "chat_template"]
        }

    def get_model_args(self):
        return {k: v for k, v in self.__dict__.items() if k not in ["chat_template"]}


@dataclass
class QuantizationConfig(BitsAndBytesConfig):
    """
    Quanitization Configuration
    """

    quant_method: str = field(
        default="bitsandbytes",
        metadata={"help": "Quantization Method {bitsandbytes,gptq,awq}"},
    )
    load_in_8bit: bool = field(
        default=False, metadata={"help": "Enable 8-bit quantization"}
    )
    load_in_4bit: bool = field(
        default=False, metadata={"help": "Enable 4-bit quantization"}
    )
    llm_int8_threshold: float = field(
        default=6.0, metadata={"help": "LLM.int8 threshold"}
    )
    llm_int8_skip_modules: list[str] = field(
        default=None, metadata={"help": "Modules to skip for 8-bit conversion"}
    )
    llm_int8_enable_fp32_cpu_offload: bool = field(
        default=False, metadata={"help": "Enable FP32 CPU offload for 8-bit"}
    )
    llm_int8_has_fp16_weight: bool = field(
        default=False, metadata={"help": "Use FP16 weights for LLM.int8"}
    )
    bnb_4bit_compute_dtype: str = field(
        default="float32", metadata={"help": "Compute dtype for 4-bit quantization"}
    )
    bnb_4bit_quant_type: str = field(
        default="fp4", metadata={"help": "Quantization type for 4-bit"}
    )
    bnb_4bit_use_double_quant: bool = field(
        default=False, metadata={"help": "Use double quantization for 4-bit"}
    )


# class CheckpointCallback(TrainerCallback):
#     def on_train_end(self, args, state, control, **kwargs):
#         model_path = args.output_dir
#         timestamp = datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
#         img_tag = f"ghcr.io/YOUR_USERNAME/LoRA-Adapter:{timestamp}"

#         # Write a file to indicate fine_tuning completion
#         completion_indicator_path = os.path.join(model_path, "training_completed.txt")
#         with open(completion_indicator_path, 'w') as f:
#             f.write(f"Training completed at {timestamp}\n")
#             f.write(f"Image Tag: {img_tag}\n")

# This method is called whenever a checkpoint is saved.
# def on_save(self, args, state, control, **kwargs):
#     docker_build_and_push()
