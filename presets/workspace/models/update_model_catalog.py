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

"""Update model_catalog.yaml with latest values from HuggingFace.

Usage:
    python update_model_catalog.py                              # Update all existing entries
    python update_model_catalog.py --repos org/m1,org/m2        # Add or update specific repos
    python update_model_catalog.py --dry-run                    # Show changes without writing
    python update_model_catalog.py --token <HF_TOKEN>           # Use auth token
"""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import sys
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen

HUGGINGFACE_URL = "https://huggingface.co"
CATALOG_FILE = Path(__file__).parent / "model_catalog.yaml"

SAFETENSOR_RE = re.compile(r".*\.safetensors$")
BIN_RE = re.compile(r".*\.bin$")
MISTRAL_RE = re.compile(r"consolidated.*\.safetensors$")

# Keys tried in order for each config field (mirrors Go generator.go getInt logic)
CONFIG_KEY_MAP = {
    "modelTokenLimit": [
        "max_position_embeddings",
        "n_ctx",
        "seq_length",
        "max_seq_len",
        "max_sequence_length",
    ],
    "hiddenSize": ["hidden_size", "n_embd", "d_model"],
    "numHiddenLayers": ["num_hidden_layers", "n_layer", "n_layers"],
    "numAttentionHeads": ["num_attention_heads", "n_head", "n_heads"],
    "numKeyValueHeads": ["num_key_value_heads", "n_head_kv", "n_kv_heads"],
}

OPTIONAL_KEY_MAP = {
    "headDim": ["head_dim"],
    "kvLoraRank": ["kv_lora_rank"],
    "qkRopeHeadDim": ["qk_rope_head_dim"],
}


def fetch_json(url: str, token: str | None = None) -> dict | list:
    """Fetch JSON from a URL with optional Bearer auth."""
    req = Request(url)
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    with urlopen(req, timeout=30) as resp:  # noqa: S310
        return json.loads(resp.read())


def get_config_value(config: dict, keys: list[str], default=None):
    """Return the first matching key's value from config."""
    for key in keys:
        if key in config:
            val = config[key]
            if isinstance(val, float):
                return int(val)
            return val
    return default


def compute_model_file_size(files: list[dict]) -> tuple[str, bool]:
    """Compute total model file size and detect Mistral format.

    Returns (size_string like "10Gi", is_mistral).
    """
    selected = []
    mistral_files = []

    for f in files:
        path = f.get("path", "")
        if MISTRAL_RE.match(path):
            mistral_files.append(f)
        if SAFETENSOR_RE.match(path) or BIN_RE.match(path):
            selected.append(f)

    is_mistral = len(mistral_files) > 0
    if is_mistral:
        selected = mistral_files
    elif selected:
        has_safetensors = any(f["path"].endswith(".safetensors") for f in selected)
        if has_safetensors:
            selected = [f for f in selected if f["path"].endswith(".safetensors")]
    else:
        raise ValueError("No .safetensors or .bin files found")

    total_bytes = sum(f.get("size", 0) for f in selected)
    size_gib = math.ceil(total_bytes / (1024**3))
    return f"{size_gib}Gi", is_mistral


def fetch_entry(repo: str, token: str | None = None) -> dict:
    """Fetch catalog entry values from HuggingFace for a model repo."""
    # Fetch file listing
    files_url = f"{HUGGINGFACE_URL}/api/models/{repo}/tree/main?recursive=true"
    files = fetch_json(files_url, token)

    model_file_size, is_mistral = compute_model_file_size(files)

    # Fetch config
    config_file = "params.json" if is_mistral else "config.json"
    config_url = f"{HUGGINGFACE_URL}/{repo}/resolve/main/{config_file}"
    config = fetch_json(config_url, token)

    entry = {
        "modelRepo": repo,
        "modelFileSize": model_file_size,
    }

    # Architectures
    archs = config.get("architectures", [])
    if archs:
        entry["architectures"] = archs

    # Required fields from config
    for catalog_key, config_keys in CONFIG_KEY_MAP.items():
        val = get_config_value(config, config_keys)
        if val is not None:
            entry[catalog_key] = val

    # Optional fields - only include if present
    for catalog_key, config_keys in OPTIONAL_KEY_MAP.items():
        val = get_config_value(config, config_keys)
        if val is not None and val > 0:
            entry[catalog_key] = val

    # Check if headDim needs to be explicit
    # (only when it differs from hiddenSize / numAttentionHeads)
    hidden = entry.get("hiddenSize", 0)
    heads = entry.get("numAttentionHeads", 0)
    head_dim = entry.get("headDim")
    if head_dim and heads > 0 and hidden > 0 and head_dim == hidden // heads:
        # headDim matches the default derivation, no need to store it
        del entry["headDim"]

    # Mistral format fields
    if is_mistral:
        entry["loadFormat"] = "mistral"
        entry["configFormat"] = "mistral"
        entry["tokenizerMode"] = "mistral"

    return entry


def load_catalog(path: Path) -> list[dict]:
    """Load existing catalog entries from YAML file."""
    # Minimal YAML parsing to avoid PyYAML dependency for simple structure
    if not path.exists():
        return []

    import yaml

    with open(path) as f:
        data = yaml.safe_load(f)

    return data.get("models", []) if data else []


def save_catalog(path: Path, entries: list[dict]) -> None:
    """Write catalog entries to YAML file."""
    import yaml

    data = {"models": entries}
    with open(path, "w") as f:
        yaml.dump(
            data,
            f,
            default_flow_style=None,
            sort_keys=False,
            allow_unicode=True,
        )


def format_diff(old: dict | None, new: dict) -> str:
    """Format a human-readable diff between old and new entry."""
    repo = new["modelRepo"]
    if old is None:
        lines = [f"  + ADD {repo}"]
        for k, v in new.items():
            if k != "modelRepo":
                lines.append(f"      {k}: {v}")
        return "\n".join(lines)

    changes = []
    all_keys = set(list(old.keys()) + list(new.keys())) - {"modelRepo"}
    for k in sorted(all_keys):
        old_val = old.get(k)
        new_val = new.get(k)
        if old_val != new_val:
            changes.append(f"      {k}: {old_val!r} -> {new_val!r}")

    if not changes:
        return f"  = {repo} (no changes)"

    return f"  ~ UPDATE {repo}\n" + "\n".join(changes)


def main():
    parser = argparse.ArgumentParser(
        description="Update model_catalog.yaml from HuggingFace"
    )
    parser.add_argument(
        "--repos",
        type=str,
        default="",
        help="Comma-separated list of repos to add or update (default: all existing)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show changes without writing to file",
    )
    parser.add_argument(
        "--token",
        type=str,
        default=os.environ.get("HF_TOKEN", ""),
        help="HuggingFace API token (default: $HF_TOKEN)",
    )
    parser.add_argument(
        "--catalog",
        type=str,
        default=str(CATALOG_FILE),
        help="Path to model_catalog.yaml",
    )
    args = parser.parse_args()

    catalog_path = Path(args.catalog)
    token = args.token or None
    existing = load_catalog(catalog_path)
    existing_by_repo = {e["modelRepo"].lower(): e for e in existing}

    # Determine which repos to process
    if args.repos:
        target_repos = [r.strip() for r in args.repos.split(",") if r.strip()]
    else:
        target_repos = [e["modelRepo"] for e in existing]

    if not target_repos:
        print("No repos to process. Use --repos to specify, or populate the catalog.")
        sys.exit(0)

    updated = list(existing)
    has_changes = False

    for repo in target_repos:
        print(f"Fetching {repo}...")
        try:
            new_entry = fetch_entry(repo, token)
        except (HTTPError, ValueError, json.JSONDecodeError) as e:
            print(f"  ERROR: {e}", file=sys.stderr)
            continue

        old_entry = existing_by_repo.get(repo.lower())
        diff = format_diff(old_entry, new_entry)
        print(diff)

        if old_entry is None:
            # New entry
            updated.append(new_entry)
            existing_by_repo[repo.lower()] = new_entry
            has_changes = True
        elif "no changes" not in diff:
            # Update in place
            for i, e in enumerate(updated):
                if e["modelRepo"].lower() == repo.lower():
                    updated[i] = new_entry
                    break
            has_changes = True

    if not has_changes:
        print("\nNo changes detected.")
        return

    if args.dry_run:
        print("\n--dry-run: no file written.")
    else:
        save_catalog(catalog_path, updated)
        print(f"\nWritten to {catalog_path}")


if __name__ == "__main__":
    main()
