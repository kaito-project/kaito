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

import logging
import os
from dataclasses import asdict
from datetime import datetime

import torch
from accelerate import Accelerator
from cli import DatasetConfig, ExtDataCollator, ExtLoraConfig, ModelConfig
from dataset import DatasetManager
from parser import load_chat_template, parse_configs
from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    BitsAndBytesConfig,
    TrainerCallback,
    TrainerControl,
    TrainerState,
    TrainingArguments,
)
from trl import SFTTrainer

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get("DEBUG_MODE", "false").lower() == "true"
logging.basicConfig(
    level=logging.DEBUG if debug_mode else logging.INFO,
    format="%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s",
    datefmt="%m-%d %H:%M:%S",
)

CONFIG_YAML = os.environ.get("YAML_FILE_PATH", "/mnt/config/training_config.yaml")
parsed_configs = parse_configs(CONFIG_YAML)

model_config: ModelConfig = parsed_configs.get("ModelConfig")
bnb_config: BitsAndBytesConfig = parsed_configs.get("QuantizationConfig")
ext_lora_config: ExtLoraConfig = parsed_configs.get("LoraConfig")
ta_args: TrainingArguments = parsed_configs.get("TrainingArguments")
ds_config: DatasetConfig = parsed_configs.get("DatasetConfig")
dc_args: ExtDataCollator = parsed_configs.get("DataCollator")

accelerator = Accelerator()

# Load Model Args
model_args = model_config.get_model_args()
if accelerator.distributed_type != "NO":  # Meaning we require distributed training
    logger.debug("Setting device map for distributed training")
    model_args["device_map"] = {"": accelerator.process_index}

# Load BitsAndBytesConfig
bnb_config_args = asdict(bnb_config)
bnb_config = BitsAndBytesConfig(**bnb_config_args)
enable_qlora = bnb_config.is_quantizable()

# Load the Pre-Trained Tokenizer
tokenizer_args = model_config.get_tokenizer_args()
resovled_chat_template = load_chat_template(model_config.chat_template)
tokenizer = AutoTokenizer.from_pretrained(**tokenizer_args)
if not tokenizer.pad_token:
    tokenizer.pad_token = tokenizer.eos_token
if resovled_chat_template is not None:
    tokenizer.chat_template = resovled_chat_template
if dc_args.mlm and tokenizer.mask_token is None:
    logger.warning(
        "This tokenizer does not have a mask token which is necessary for masked language modeling. "
        "You should pass `mlm=False` to train on causal language modeling instead. "
        "Setting mlm=False"
    )
    dc_args.mlm = False
dc_args.tokenizer = tokenizer

# Load the Pre-Trained Model
model = AutoModelForCausalLM.from_pretrained(
    **model_args,
    quantization_config=bnb_config if enable_qlora else None,
)

logger.info("Model Loaded")

if enable_qlora:
    # Preparing the Model for QLoRA
    model = prepare_model_for_kbit_training(model)
    logger.info("QLoRA Enabled")

if not ext_lora_config:
    logger.error("LoraConfig must be specified")
    raise ValueError("LoraConfig must be specified")

lora_config_args = asdict(ext_lora_config)
lora_config = LoraConfig(**lora_config_args)

model = get_peft_model(model, lora_config)
# Cache is only used for generation, not for training
model.config.use_cache = False
model.print_trainable_parameters()

dm = DatasetManager(ds_config)
# Load the dataset
dm.load_data()
if not dm.get_dataset():
    logger.error("Failed to load dataset.")
    raise ValueError("Unable to load the dataset.")

# Shuffling the dataset (if needed)
if ds_config.shuffle_dataset:
    dm.shuffle_dataset()

train_dataset, eval_dataset = dm.split_dataset()


class EmptyCacheCallback(TrainerCallback):
    def on_step_end(self, args, state: TrainerState, control: TrainerControl, **kwargs):
        torch.cuda.empty_cache()
        return control


empty_cache_callback = EmptyCacheCallback()

# Prepare for training
torch.cuda.set_device(accelerator.process_index)
torch.cuda.empty_cache()
# Training the Model
trainer: SFTTrainer = accelerator.prepare(
    SFTTrainer(
        model=model,
        tokenizer=tokenizer,
        train_dataset=train_dataset,
        eval_dataset=eval_dataset,
        args=ta_args,
        data_collator=dc_args,
        dataset_text_field=dm.dataset_text_field,
        callbacks=[empty_cache_callback],
        # metrics = "tensorboard" or "wandb" # TODO
    )
)
trainer.train()
os.makedirs(ta_args.output_dir, exist_ok=True)
# only save the adapter weights
trainer.model.save_pretrained(ta_args.output_dir)

# Write file to signify training completion
timestamp = datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
logger.info("Fine-Tuning completed\n")
completion_indicator_path = os.path.join(
    ta_args.output_dir, "fine_tuning_completed.txt"
)
with open(completion_indicator_path, "w") as f:
    f.write(f"Fine-Tuning completed at {timestamp}\n")
