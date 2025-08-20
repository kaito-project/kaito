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

from datasets import load_dataset

SUPPORTED_EXTENSIONS = {"csv", "json", "parquet", "arrow", "webdataset"}


class DatasetManager:
    def __init__(self, config):
        self.config = config
        self.dataset = None
        self.dataset_text_field = (
            None  # Set this field if dataset consists of singular text column
        )

    def check_dataset_loaded(self):
        if self.dataset is None:
            raise ValueError("Dataset is not loaded.")

    def check_column_exists(self, column_name):
        if column_name not in self.dataset.column_names:
            raise ValueError(
                f"Column '{column_name}' does not exist in the dataset. Available columns: {self.dataset.column_names}"
            )

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
            dataset_path = self.find_valid_dataset(
                os.environ.get("DATASET_FOLDER_PATH", "/mnt/data")
            )
            if not dataset_path:
                raise ValueError("Unable to find a valid dataset file.")

        file_ext = (
            self.config.dataset_extension
            if self.config.dataset_extension
            else self.get_file_extension(dataset_path)
        )
        try:
            self.dataset = load_dataset(
                file_ext, data_files=dataset_path, split="train"
            )
            print(
                f"Dataset loaded successfully from {dataset_path} with file type '{file_ext}'."
            )
        except Exception as e:
            print(f"Error loading dataset: {e}")
            raise ValueError(
                f"Unable to load dataset {dataset_path} with file type '{file_ext}'"
            )

    def find_valid_dataset(self, data_dir):
        """Searches for a file with a valid dataset type in the given directory."""
        for root, dirs, files in os.walk(data_dir):
            for file in files:
                filename_lower = file.lower()  # Convert to lowercase once per filename
                for ext in SUPPORTED_EXTENSIONS:
                    if ext in filename_lower:
                        return os.path.join(root, file)
        return None

    def get_file_extension(self, file_path):
        """Returns the file extension based on filetype guess or filename."""
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
                test_size=1 - self.config.train_test_split,
                seed=self.config.shuffle_seed,
            )
            return split_dataset["train"], split_dataset["test"]
        else:
            return self.dataset, None

    def get_dataset(self):
        self.check_dataset_loaded()
        return self.dataset
