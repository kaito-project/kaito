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

import os
import subprocess
import sys

from huggingface_hub import snapshot_download

# Custom media type for OCI artifacts
CUSTOM_MEDIA_TYPE = "application/vnd.kaito.llm.v1"


def parse_huggingface_info(model_version: str):
    """
    Parse model version string to extract repo name and revision from a HuggingFace model URL.

    Args:
        model_version: A HuggingFace URL that may include commit, branch, or tag information

    Returns:
        tuple: (repo_name, revision) where:
            - repo_name: Name of the repository (e.g. 'mistralai/Mistral-7B-Instruct-v0.3')
            - revision: The commit hash, branch name, or tag name

    Examples:
        - "https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/commit/e0bc86c23ce5aae1db576c8cca6f06f1f73af2db"
            -> ("mistralai/Mistral-7B-Instruct-v0.3", "e0bc86c23ce5aae1db576c8cca6f06f1f73af2db")
        - "https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/tree/main"
            -> ("mistralai/Mistral-7B-Instruct-v0.3", "main")
        - "https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3"
            -> ("mistralai/Mistral-7B-Instruct-v0.3", "main")
    """
    # Extract everything after huggingface.co/
    parts = model_version.split("huggingface.co/", 1)
    if len(parts) < 2:
        return None, "main"

    path_parts = parts[1].split("/")

    # At minimum, we need the org/repo part
    if len(path_parts) < 2:
        return parts[1], "main"

    # Build the repo name (handle cases where repo name has multiple parts)
    repo_name_parts = []
    revision = "main"  # Default revision

    # Process path parts to extract repo name and revision
    i = 0
    while i < len(path_parts):
        if path_parts[i] in ["commit", "tree", "blob", "tag"]:
            if i + 1 < len(path_parts):
                revision = path_parts[i + 1]
            break
        repo_name_parts.append(path_parts[i])
        i += 1

    repo_name = "/".join(repo_name_parts)

    return repo_name, revision


def push_to_oras(source_dir, image_name):
    """
    Push files from a directory to an OCI registry using ORAS CLI.

    Args:
        source_dir: Directory containing files to push
        image_name: Target OCI image name including registry
    """
    try:
        # Initialize command with oras push and image name
        # ORAS cli is much more stable than python sdk.
        command = ["oras", "push", "--concurrency", "2", image_name]

        # Gather all files with their relative paths, excluding hidden files
        files_to_push = []
        for entry in os.scandir(source_dir):
            if entry.is_file():
                files_to_push.append(f"{entry.name}:{CUSTOM_MEDIA_TYPE}")

        original_dir = os.getcwd()

        try:
            # Change to the source directory
            os.chdir(source_dir)

            command.extend(files_to_push)
            print(f"Running command: {' '.join(command)}")

            # Use Popen instead of run to get live output
            process = subprocess.Popen(
                command,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                bufsize=1,
            )

            for line in process.stdout:
                print(line, end="")

            # Wait for process to complete and check return code
            return_code = process.wait()
            if return_code != 0:
                raise subprocess.CalledProcessError(return_code, command)

            print(f"Successfully pushed files to {image_name}")
        finally:
            # Restore the original working directory
            os.chdir(original_dir)

    except subprocess.CalledProcessError as e:
        print(f"Error pushing files with ORAS: {e.stderr}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error in ORAS push operation: {e}", file=sys.stderr)
        sys.exit(1)


def main():
    # Get image name from first CLI argument
    if len(sys.argv) < 2:
        print(
            "ERROR: Please provide the target OCI image name as the first argument",
            file=sys.stderr,
        )
        sys.exit(1)

    image_name = sys.argv[1]

    # Get environment variables
    model_name = os.environ.get("MODEL_NAME")
    if not model_name:
        print("ERROR: MODEL_NAME environment variable is required", file=sys.stderr)
        sys.exit(1)

    model_version = os.environ.get("MODEL_VERSION")
    if not model_version:
        print("ERROR: MODEL_VERSION environment variable is required", file=sys.stderr)
        sys.exit(1)

    weights_dir = os.environ.get("WEIGHTS_DIR", "/tmp/")
    if not weights_dir:
        print(
            "ERROR: WEIGHTS_DIR environment variable cannot be empty", file=sys.stderr
        )
        sys.exit(1)
    hf_token = os.environ.get("HF_TOKEN", "")

    repo_name, revision = parse_huggingface_info(model_version)
    print(f"Downloading model {repo_name} to {weights_dir}...")
    try:
        # Download the model
        snapshot_download(
            repo_id=repo_name,
            revision=revision,
            local_dir=weights_dir,
            token=hf_token if hf_token else None,
        )

    except Exception as e:
        print(f"Error downloading model: {e}", file=sys.stderr)
        sys.exit(1)
    print("Model downloaded successfully.")

    # Push to OCI registry
    print(f"Pushing model files to {image_name}...")
    push_to_oras(weights_dir, image_name)


if __name__ == "__main__":
    main()
