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

#!/usr/bin/env python3
import argparse
import json
import os
import sys
from pathlib import Path

import yaml


def get_project_root():
    # Get the directory containing the current script
    current_dir = Path(__file__).resolve().parent
    # Go up 4 levels to reach project root (from presets/workspace/test/scripts)
    return current_dir.parents[3]


def load_json_config():
    project_root = get_project_root()
    config_path = project_root / ".github" / "e2e-preset-configs.json"
    with open(config_path) as f:
        data = json.load(f)
        return data["matrix"]["image"]  # Return the array of model configs


def load_template():
    project_root = get_project_root()
    template_path = os.path.join(
        project_root,
        "presets",
        "workspace",
        "test",
        "manifests",
        "inference-tmpl",
        "manifest.yaml",
    )
    with open(template_path) as f:
        return yaml.safe_load(f)


def check_predefined_manifest(model_name):
    project_root = get_project_root()
    manifest_path = os.path.join(
        str(project_root),
        "presets",
        "workspace",
        "test",
        "manifests",
        f"{model_name}",
        f"{model_name}.yaml",
    )
    if not os.path.exists(manifest_path):
        return (None, False)
    with open(manifest_path) as f:
        return (f.read(), True)


def process_model(model_name, runtime, repo=None, tag=None):
    configs = load_json_config()
    model_config = next((m for m in configs if m["name"] == model_name), None)

    if not model_config:
        print(f"Model {model_name} not found in configs", file=sys.stderr)
        sys.exit(1)

    predefined_manifest, predefined_manifest_exists = check_predefined_manifest(
        model_name
    )
    if predefined_manifest_exists:
        return process_predefined_manifest(
            model_name, runtime, predefined_manifest, repo, tag
        )

    if runtime not in model_config.get("runtimes", {}):
        print(
            f"Runtime {runtime} not configured for model {model_name}", file=sys.stderr
        )
        sys.exit(1)

    runtime_config = model_config["runtimes"][runtime]
    workload_name = model_config.get("workload", model_name)

    templates = load_template()
    manifest_str = yaml.dump(templates["deployment"])

    # Replace placeholders in template
    manifest_str = (
        manifest_str.replace("WORKLOAD_NAME", workload_name)
        .replace("MODEL_NAME", model_name)
        .replace("RUNTIME_COMMAND", runtime_config["command"])
        .replace("GPU_COUNT", str(runtime_config["gpu_count"]))
        .replace("NODE_POOL", model_config["node_pool"])
        .replace("NODE_COUNT", str(model_config["node-count"]))
    )

    # Replace repo and tag if provided
    if repo:
        manifest_str = manifest_str.replace("REPO", repo)
    if tag:
        manifest_str = manifest_str.replace("TAG", tag)

    # Parse the template string back into YAML
    manifest = yaml.safe_load(manifest_str)

    # Generate service manifest
    service_template = templates["service"]
    service_str = yaml.dump(service_template)
    service_str = service_str.replace("WORKLOAD_NAME", workload_name)
    service_manifest = yaml.safe_load(service_str)

    # Generate config manifest
    config_template = templates["config"]
    config_str = yaml.dump(config_template)
    config_manifest = yaml.safe_load(config_str)

    # Print manifests to stdout
    yaml.dump(manifest, sys.stdout, default_flow_style=False)
    print("---")  # Document separator
    yaml.dump(service_manifest, sys.stdout, default_flow_style=False)
    print("---")  # Document separator
    yaml.dump(config_manifest, sys.stdout, default_flow_style=False)


def process_predefined_manifest(
    model_name, runtime, predefined_manifest, repo=None, tag=None
):
    if repo:
        predefined_manifest = predefined_manifest.replace("REPO", repo)
    if tag:
        predefined_manifest = predefined_manifest.replace("TAG", tag)

    print(predefined_manifest)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Process model template with optional repo and tag."
    )
    parser.add_argument("model_name", help="Name of the model")
    parser.add_argument("runtime", help="Runtime to use")
    parser.add_argument("--repo", help="Repository name to use instead of REPO")
    parser.add_argument("--tag", help="Tag to use instead of TAG")

    args = parser.parse_args()
    process_model(args.model_name, args.runtime, args.repo, args.tag)
