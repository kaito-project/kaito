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
import shutil
import subprocess
import uuid
from pathlib import Path

import yaml


def read_yaml(file_path):
    try:
        with open(file_path) as file:
            data = yaml.safe_load(file)
            return data
    except (OSError, yaml.YAMLError) as e:
        print(f"Error reading {file_path}: {e}")
        return None


supp_models_yaml = "presets/workspace/models/supported_models.yaml"
YAML_PR = read_yaml(supp_models_yaml)
# Format: {falcon-7b : {model_name:falcon-7b, type:text-generation, version: #, tag: #}}
MODELS = {model["name"]: model for model in YAML_PR["models"]}
KAITO_REPO_URL = "https://github.com/kaito-project/kaito.git"
GITREMOTE_TARGET = "_ciupstream"


def set_multiline_output(name, value):
    if not os.getenv("GITHUB_OUTPUT"):
        print("Not in github env, skip writing to $GITHUB_OUTPUT .")
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
            print(f"Error: {error.decode('utf-8')}")
            return None
        return output.decode("utf-8").strip()
    except Exception as e:
        print(f"An error occurred: {e}")
        return None


def get_yaml_from_branch(branch, file_path):
    """Read YAML from a branch"""
    subprocess.run(["git", "fetch", GITREMOTE_TARGET, branch], check=True)
    subprocess.run(["git", "checkout", f"{GITREMOTE_TARGET}/" + branch], check=True)
    content = read_yaml(file_path)
    subprocess.run(["git", "checkout", "-"], check=True)
    return content


def detect_changes_in_yaml(yaml_main, yaml_pr):
    """Detecting relevant changes in support_models.yaml"""
    yaml_main, yaml_pr = yaml_main["models"], yaml_pr["models"]

    models_to_build = []
    for model_pr in yaml_pr:
        # Searches for matching models
        model_main = next((m for m in yaml_main if m["name"] == model_pr["name"]), None)
        # New Model
        if not model_main:
            print("New Model: ", model_pr["name"])
            models_to_build.append(model_pr["name"])
        # Model Version Update
        elif model_pr.get("version") != model_main.get("version"):
            print("Updated Version of Model: ", model_pr["name"])
            models_to_build.append(model_pr["name"])
        # Model Tag Update
        elif model_pr.get("tag") != model_main.get("tag"):
            print("Update Tag of Model: ", model_pr["name"])
            models_to_build.append(model_pr["name"])
    return models_to_build


def models_to_build(files_changed):
    """Models to build based on changed files."""
    models, seen_model_types = set(), set()
    if supp_models_yaml in files_changed:
        yaml_main = get_yaml_from_branch("main", supp_models_yaml)
        models.update(detect_changes_in_yaml(yaml_main, YAML_PR))
    for model, model_info in MODELS.items():
        if model_info["type"] not in seen_model_types and any(
            file.startswith(f"presets/workspace/inference/{model_info['type']}")
            for file in files_changed
        ):
            models.add(model)
            seen_model_types.add(model_info["type"])
    return list(models)


def check_modified_models():
    """Check for modified models in the repository."""
    repo_dir = Path.cwd() / "repo"

    if repo_dir.exists():
        shutil.rmtree(repo_dir)

    run_command(f"git remote add {GITREMOTE_TARGET} {KAITO_REPO_URL}")
    run_command(f"git fetch {GITREMOTE_TARGET}")

    files = run_command(
        f"git diff --name-only {GITREMOTE_TARGET}/main"
    )  # Returns each file on newline
    files = files.split("\n")
    print("Files Changed: ", files)

    modified_models = models_to_build(files)

    print("Modified Models (Images to build): ", modified_models)

    return modified_models


def main():
    force_run_all = os.environ.get(
        "FORCE_RUN_ALL", "false"
    )  # If not specified default to False
    force_run_all_phi = os.environ.get(
        "FORCE_RUN_ALL_PHI", "false"
    )  # If not specified default to False

    affected_models = []
    if force_run_all != "false":
        affected_models = [
            model["name"] for model in YAML_PR["models"] if model["name"]
        ]
    elif force_run_all_phi != "false":
        affected_models = [
            model["name"] for model in YAML_PR["models"] if "phi-3" in model["name"]
        ]
    else:
        # Logic to determine affected models
        # Example: affected_models = ['model1', 'model2', 'model3']
        affected_models = check_modified_models()

    download_at_runtime = {
        model["name"] for model in YAML_PR["models"] if model.get("downloadAtRuntime")
    }
    affected_models = [
        model for model in affected_models if model not in download_at_runtime
    ]

    # Convert the list of models into JSON matrix format
    matrix = create_matrix(affected_models)
    print("Create Matrix: ", matrix)

    # Set the matrix as an output for the GitHub Actions workflow
    set_multiline_output("matrix", matrix)


if __name__ == "__main__":
    main()
