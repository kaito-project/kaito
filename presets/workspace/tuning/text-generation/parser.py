# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import logging
from dataclasses import asdict, fields
import codecs
from pathlib import Path
from typing import Optional

import yaml
from cli import (DatasetConfig, ExtDataCollator, ExtLoraConfig, ModelConfig, QuantizationConfig)
from transformers import HfArgumentParser, TrainingArguments

logger = logging.getLogger(__name__)

# Mapping from config section names to data classes
CONFIG_CLASS_MAP = {
    'ModelConfig': ModelConfig,
    'QuantizationConfig': QuantizationConfig,
    'LoraConfig': ExtLoraConfig,
    'TrainingArguments': TrainingArguments,
    'DatasetConfig': DatasetConfig,
    'DataCollator': ExtDataCollator,
}

def flatten_config_to_cli_args(config, prefix=''):
    cli_args = []
    for key, value in config.items():
        if isinstance(value, dict):
            cli_args.extend(flatten_config_to_cli_args(value, prefix=f'{prefix}{key}_'))
        elif isinstance(value, list):
            cli_arg = f'--{prefix}{key}'
            cli_args.append(cli_arg)
            cli_args.extend(map(str, value))
        else:
            cli_arg = f'--{prefix}{key}'
            cli_args.append(cli_arg)
            cli_args.append(str(value))
    return cli_args


def filter_unsupported_init_args(dataclass_type, config_dict):
    supported_fields = {f.name for f in fields(dataclass_type) if f.init}
    filtered_config = {k: v for k, v in config_dict.items() if k in supported_fields}
    return filtered_config


# Function to parse a single section
def parse_section(section_name, section_config):
    parser = HfArgumentParser((CONFIG_CLASS_MAP[section_name],))
    # Flatten section_config to CLI-like arguments
    cli_args = flatten_config_to_cli_args(section_config, prefix='')
    # Parse the CLI-like arguments into a data class instance
    return parser.parse_args_into_dataclasses(cli_args)[0]


def parse_configs(config_yaml):
    # Load the YAML configuration
    with open(config_yaml, 'r') as file:
        full_config = yaml.safe_load(file)
    training_config = full_config.get('training_config', {})
    print("training_config:", training_config)

    # Parse and merge configurations
    parsed_configs = {}
    for section_name, class_type in CONFIG_CLASS_MAP.items():
        # Parse section from YAML
        yaml_parsed_instance = parse_section(section_name, training_config.get(section_name, {}))
        yaml_parsed_dict = asdict(yaml_parsed_instance)
        merged_config = yaml_parsed_dict

        filtered_config = filter_unsupported_init_args(CONFIG_CLASS_MAP[section_name], merged_config)
        parsed_configs[section_name] = CONFIG_CLASS_MAP[section_name](**filtered_config)

    return parsed_configs

def load_chat_template(chat_template: Optional[str]) -> Optional[str]:
    logger.info(chat_template)
    if chat_template is None:
        return None

    JINJA_CHARS = "{}\n"
    if any(c in chat_template for c in JINJA_CHARS):
        resolved_chat_template = codecs.decode(chat_template, "unicode_escape")
    else:
        resolved_chat_template = Path(chat_template).read_text()

    logger.info("Chat template loaded successfully")
    logger.info("Chat template:\n%s", resolved_chat_template)
    return resolved_chat_template
