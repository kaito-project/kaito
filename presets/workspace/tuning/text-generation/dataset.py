# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import os
import logging
from typing import Optional

from datasets import load_dataset

SUPPORTED_EXTENSIONS = {'csv', 'json', 'parquet', 'arrow', 'webdataset'}
logger = logging.getLogger(__name__)

class DatasetManager:
    def __init__(self, config):
        self.config = config
        self.dataset = None
        self.dataset_text_field = None

    def check_dataset_loaded(self):
        if self.dataset is None:
            raise ValueError("Dataset is not loaded.")

    def check_column_exists(self, column_name):
        if column_name not in self.dataset.column_names:
            raise ValueError(f"Column '{column_name}' does not exist in the dataset. Available columns: {self.dataset.column_names}")

    def select_and_rename_columns(self, columns_to_select, rename_map=None):
        self.dataset = self.dataset.select_columns(columns_to_select)
        if rename_map:
            for old_name, new_name in rename_map.items():
                if old_name != new_name:
                    self.dataset = self.dataset.rename_column(old_name, new_name)

    def load_data(self):
        # OAI Compliant: https://platform.openai.com/docs/guides/fine-tuning/preparing-your-dataset
        # https://github.com/huggingface/trl/blob/main/trl/extras/dataset_formatting.py
        # https://huggingface.co/docs/trl/en/sft_trainer#dataset-format-support
        if self.config.dataset_path:
            dataset_path = os.path.join("/mnt", self.config.dataset_path.strip("/"))
        else:
            dataset_path = self.find_valid_dataset(os.environ.get('DATASET_FOLDER_PATH', '/mnt/data'))
            if not dataset_path:
                raise ValueError("Unable to find a valid dataset file.")

        file_ext = self.config.dataset_extension if self.config.dataset_extension else self.get_file_extension(dataset_path)
        try:
            self.dataset = load_dataset(file_ext, data_files=dataset_path, split="train")
            logger.info(f"Dataset loaded successfully from {dataset_path} with file type '{file_ext}'.")
            
            # Set the dataset_text_field based on the configuration
            if self.config.response_column and self.config.response_column in self.dataset.column_names:
                self.dataset_text_field = self.config.response_column
            elif self.config.messages_column and self.config.messages_column in self.dataset.column_names:
                self.dataset_text_field = self.config.messages_column
            elif self.config.context_column and self.config.context_column in self.dataset.column_names:
                self.dataset_text_field = self.config.context_column
            elif len(self.dataset.column_names) == 1:
                # If there's only one column, use that
                self.dataset_text_field = self.dataset.column_names[0]
            else:
                # Default to the first column as a fallback
                self.dataset_text_field = self.dataset.column_names[0]
                logger.warning(f"No specific text field configured. Using '{self.dataset_text_field}' as default.")
                
            logger.info(f"Using '{self.dataset_text_field}' as the dataset text field.")
            
        except Exception as e:
            logger.error(f"Error loading dataset: {e}")
            raise ValueError(f"Unable to load dataset {dataset_path} with file type '{file_ext}'")

    def find_valid_dataset(self, data_dir):
        """ Searches for a file with a valid dataset type in the given directory. """
        for root, dirs, files in os.walk(data_dir):
            for file in files:
                filename_lower = file.lower()  # Convert to lowercase once per filename
                for ext in SUPPORTED_EXTENSIONS:
                    if ext in filename_lower:
                        return os.path.join(root, file)
        return None

    def get_file_extension(self, file_path):
        """ Returns the file extension based on filetype guess or filename. """
        filename_lower = os.path.basename(file_path).lower()
        for ext in SUPPORTED_EXTENSIONS:
            if ext in filename_lower:
                return ext
        _, file_ext = os.path.splitext(file_path)
        return file_ext[1:]  # Remove leading "."

    def shuffle_dataset(self, seed=None):
        self.check_dataset_loaded()
        self.dataset = self.dataset.shuffle(seed=seed)

    def split_dataset(self):
        self.check_dataset_loaded()
        if not (0 < self.config.train_test_split <= 1):
            raise ValueError("Train/Test split needs to be between 0 and 1")
        if self.config.train_test_split < 1:
            split_dataset = self.dataset.train_test_split(
                test_size=1-self.config.train_test_split,
                seed=self.config.shuffle_seed
            )
            return split_dataset['train'], split_dataset['test']
        else:
            return self.dataset, None

    def get_dataset(self):
        self.check_dataset_loaded()
        return self.dataset