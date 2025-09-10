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
import sys

import requests


def get_token():
    for arg in sys.argv:
        if arg.startswith("--token="):
            return arg.split("=", 1)[1]
    return os.environ.get("HF_TOKEN")


def make_request(url, method="GET", token=None):
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"

    if method == "HEAD":
        return requests.head(url, headers=headers, allow_redirects=True)
    return requests.get(url, headers=headers)


def list_safetensors_files(model_repo, token=None):
    api_url = f"https://huggingface.co/api/models/{model_repo}"
    response = make_request(api_url, method="GET", token=token)
    response.raise_for_status()
    data = response.json()

    safetensor_files = [
        file["rfilename"]
        for file in data.get("siblings", [])
        if file["rfilename"].endswith(".safetensors")
    ]
    return safetensor_files


def list_bin_files(model_repo, token=None):
    api_url = f"https://huggingface.co/api/models/{model_repo}"
    response = make_request(api_url, method="GET", token=token)
    response.raise_for_status()
    data = response.json()

    bin_files = [
        file["rfilename"]
        for file in data.get("siblings", [])
        if file["rfilename"].endswith(".bin")
    ]
    return bin_files


def get_file_size_bytes(model_repo, filename, token=None):
    url = f"https://huggingface.co/{model_repo}/resolve/main/{filename}"
    response = make_request(url, method="HEAD", token=token)
    response.raise_for_status()
    size_bytes = int(response.headers.get("Content-Length", 0))
    return size_bytes


def get_total_safetensors_size(model_repo, token=None):
    # Special case for falcon-40b-instruct: use .bin files instead of .safetensors
    if model_repo == "tiiuae/falcon-40b-instruct":
        filenames = list_bin_files(model_repo, token=token)
        print(f"[Special case] Using .bin files for {model_repo}")
    else:
        filenames = list_safetensors_files(model_repo, token=token)

    total_bytes = 0
    for name in filenames:
        try:
            size = get_file_size_bytes(model_repo, name, token=token)
            size_gb = size / (1000**3)
            size_gib = size / (1024**3)
            print(
                f"Found: {name} - {size_gb:.2f} GB (decimal), {size_gib:.2f} GiB (binary)"
            )
            total_bytes += size
        except Exception as e:
            print(f"[Warning] Could not get size for {name}: {e}")

    total_gb = total_bytes / (1000**3)
    total_gib = total_bytes / (1024**3)
    return total_gb, total_gib


def compute_formula(model_repo, token=None):
    config_url = f"https://huggingface.co/{model_repo}/resolve/main/config.json"
    response = make_request(config_url, method="GET", token=token)
    response.raise_for_status()
    config = response.json()

    hidden_layers = config["num_hidden_layers"]
    hidden_size = config["hidden_size"]
    attention_heads = config["num_attention_heads"]

    # Determine kv_heads
    if "num_key_value_heads" in config:
        kv_heads = config["num_key_value_heads"]
    elif config.get("multi_query", False):
        kv_heads = 1
    else:
        kv_heads = attention_heads

    result = 2 * hidden_layers * kv_heads * (hidden_size / attention_heads) * 2
    return result


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 model.py <model_repo> [--token=YOUR_HF_TOKEN]")
        sys.exit(1)

    model_repo = sys.argv[1]
    token = get_token()

    print(f"Model: {model_repo}")
    if token:
        print("[Info] Hugging Face token provided.")
    else:
        print("[Info] No token provided, using anonymous access.")

    try:
        total_gb, total_gib = get_total_safetensors_size(model_repo, token=token)
        print(
            f"Model weight size: {total_gb:.2f} GB (decimal), {total_gib:.2f} GiB (binary)"
        )

        formula_result = compute_formula(model_repo, token=token)
        print(f"Formula result: {formula_result}")

    except requests.exceptions.HTTPError as e:
        if e.response.status_code == 401:
            print("[Error] 401 Unauthorized. This model requires access permissions.")
            print(
                "        Provide a valid Hugging Face token via --token=YOUR_TOKEN or HF_TOKEN env var."
            )
        elif e.response.status_code == 404:
            print("[Error] 404 Not Found. Check if the model name is correct.")
        else:
            print(f"[HTTP Error] {e}")
    except Exception as e:
        print(f"[Error] {e}")
