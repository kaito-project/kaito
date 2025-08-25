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

import json
import os
import subprocess
import uuid

import yaml


def read_yaml(file_path):
    try:
        with open(file_path) as file:
            data = yaml.safe_load(file)
            return data
    except (OSError, yaml.YAMLError) as e:
        print(f"Error reading {file_path}: {e}")
        return None


supported_models_yaml = "presets/workspace/models/supported_models.yaml"
supported_models = read_yaml(supported_models_yaml)
MODELS = {model["name"]: model for model in supported_models["models"]}


def set_multiline_output(name, value):
    if not os.getenv("GITHUB_OUTPUT"):
        return

    with open(os.getenv("GITHUB_OUTPUT"), "a") as fh:
        delimiter = uuid.uuid1()
        print(f"{name}<<{delimiter}", file=fh)
        print(value, file=fh)
        print(delimiter, file=fh)


def create_matrix(models_list):
    """Create GitHub Matrix"""
    matrix = [MODELS[model] for model in models_list]
    return json.dumps(matrix)


def run_command(command):
    """Execute a shell command and return the output."""
    try:
        process = subprocess.Popen(
            command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=True
        )
        output, error = process.communicate()
        if process.returncode != 0:
            return None
        return output.decode("utf-8").strip()
    except Exception:
        return None


def models_to_build():
    models = []
    for model in supported_models["models"]:
        if model.get("downloadAtRuntime", False):
            continue

        # `crane ls` lists all existing tags for the given preset image
        existing_tags = run_command(
            f"crane ls mcr.microsoft.com/aks/kaito/kaito-{model['name']}"
        )
        if not existing_tags or model["tag"] not in existing_tags:
            models.append(model["name"])
    return models


def main():
    # Convert the list of models into JSON matrix format
    matrix = create_matrix(models_to_build())
    print(matrix)

    # Set the matrix as an output for the GitHub Actions workflow
    set_multiline_output("matrix", matrix)


if __name__ == "__main__":
    main()
