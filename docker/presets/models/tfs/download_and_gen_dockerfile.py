#!/usr/bin/env python3

import os
import subprocess
import sys
import tempfile
import shutil


def must_download_model_from_huggingface(
    model_name, code_revision, model_dir, hf_token=""
):
    """
    Downloads a model from Hugging Face using Docker.

    Args:
        model_name: Name of the model to download
        model_dir: Directory to save the model
        hf_token: Hugging Face token for authentication
    """
    # Base command
    docker_cmd = [
        "docker",
        "run",
        "--rm",
        "-v",
        f"{model_dir}:/data",
        "-e",
        f"HF_TOKEN={hf_token}",
        "--platform",
        "linux/amd64",
        "python:3.12-slim",
        "/bin/bash",
        "-c",
        "pip install -q --no-cache-dir huggingface_hub && "
        f"huggingface-cli download --resume-download {model_name} --local-dir /data/ --revision {code_revision}",
    ]

    # Add token parameter only if provided
    if hf_token:
        docker_cmd[-1] += f" --token $HF_TOKEN"

    try:
        subprocess.run(docker_cmd, check=True)
    except subprocess.CalledProcessError as e:
        print(f"Error downloading model: {e}", file=sys.stderr)
        sys.exit(1)


def generate_dockerfile(base_dockerfile, model_dir, size_threshold_gb=1) -> str:
    """
    Generate a Dockerfile based on the base Dockerfile with added COPY
    commands, optimized for handling large model files.

    Args:
        base_dockerfile: Path to the base Dockerfile template
        model_dir: Directory containing the model files to be copied
        size_threshold_gb: Size threshold in GB to consider a file as "large"

    Returns:
        String containing the complete Dockerfile content
    """
    with open(base_dockerfile, "r") as f:
        dockerfile_content = f.read()

    # Create a list to store additional instructions for the Dockerfile
    additional_lines = []

    # Configure model stage and set up symbolic links for weights
    additional_lines.append("\nFROM base AS model")
    additional_lines.append("RUN ln -s /workspace/weights /workspace/tfs/weights && \\")
    additional_lines.append("    ln -s /workspace/weights /workspace/vllm/weights")

    # Identify large files that exceed the threshold and small files
    large_files = []
    small_files = []
    size_threshold_bytes = size_threshold_gb * 1024 * 1024 * 1024

    # Only get files in the top level directory, ignoring subdirectories
    for item in os.listdir(model_dir):
        file_path = os.path.join(model_dir, item)
        # Skip directories
        if os.path.isdir(file_path):
            continue

        file_size = os.path.getsize(file_path)
        if file_size > size_threshold_bytes:
            large_files.append(item)
        else:
            small_files.append(item)

    # Add COPY instructions to Dockerfile
    if large_files:
        # Copy large files individually to optimize layer caching
        additional_lines.append(
            "\n# Copy large files individually for better layer caching"
        )
        for file in large_files:
            additional_lines.append(f"COPY --from=modeldir {file} /workspace/weights/")

        # Copy remaining smaller files individually
        additional_lines.append("\n# Copy remaining model files individually")
        if small_files:
            additional_lines.append(
                f'COPY --from=modeldir {" ".join(small_files)} /workspace/weights/'
            )
    else:
        # No large files found, copy all files in one command
        additional_lines.append("\n# Copy all model weights")
        additional_lines.append(f"COPY --from=modeldir * /workspace/weights/")

    # Combine base Dockerfile with additional instructions
    return dockerfile_content + "\n".join(additional_lines) + "\n"


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


def main():
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
    must_download_model_from_huggingface(repo_name, revision, weights_dir, hf_token)

    # Get the base Dockerfile path
    script_dir = os.path.dirname(os.path.abspath(__file__))
    base_dockerfile = os.path.join(script_dir, "Dockerfile")

    # Generate the Dockerfile
    dockerfile_gen = generate_dockerfile(base_dockerfile, weights_dir)
    print("Dockerfile generated successfully.")

    # Save the generated Dockerfile to a file
    output_dockerfile_path = f"./Dockerfile_{model_name}"
    with open(output_dockerfile_path, "w") as f:
        f.write(dockerfile_gen)

    print(f"Dockerfile saved to {output_dockerfile_path}")


if __name__ == "__main__":
    main()
