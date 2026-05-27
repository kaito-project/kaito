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

"""Shared test configuration and heavy-dependency mocking for vLLM inference tests.

vllm and its transitive dependencies (transformers, huggingface-hub, torch, etc.)
have strict version constraints that may conflict in CI's lightweight pip environment.
This conftest installs lightweight stubs for modules that are not exercised by
unit tests, allowing inference_api to be imported without the full vllm stack.
"""

import sys
import types
from unittest.mock import MagicMock


def _install_mock_module(name: str) -> None:
    """Install a MagicMock as a top-level module if it is not already importable."""
    if name not in sys.modules or not hasattr(sys.modules[name], "__file__"):
        sys.modules[name] = MagicMock()


def _install_mock_tree(root: str, children: list[str]) -> None:
    """Install a mock module tree (root + root.child1 + root.child1.child2 ...)."""
    _install_mock_module(root)
    for child in children:
        full = f"{root}.{child}"
        parts = full.split(".")
        for i in range(1, len(parts) + 1):
            _install_mock_module(".".join(parts[:i]))


# Only mock if vllm is not genuinely installed (i.e., CI lightweight environment).
try:
    import vllm  # noqa: F401
except (ImportError, Exception):
    # Mock the vllm module tree that inference_api.py touches at import time.
    _install_mock_tree(
        "vllm",
        [
            "entrypoints",
            "entrypoints.openai",
            "entrypoints.openai.api_server",
            "entrypoints.openai.models",
            "entrypoints.openai.models.protocol",
            "utils",
            "utils.argparse_utils",
        ],
    )

    # Mock other heavy deps that inference_api imports at module level.
    for mod in ("pynvml", "uvloop", "psutil"):
        _install_mock_module(mod)

    # Provide a minimal FlexibleArgumentParser stub so inference_api can subclass it.
    import argparse

    fap_mod = types.ModuleType("vllm.utils.argparse_utils")
    fap_mod.FlexibleArgumentParser = argparse.ArgumentParser
    sys.modules["vllm.utils.argparse_utils"] = fap_mod

    # Provide LoRAModulePath stub.
    protocol_mod = types.ModuleType("vllm.entrypoints.openai.models.protocol")
    protocol_mod.LoRAModulePath = MagicMock()
    sys.modules["vllm.entrypoints.openai.models.protocol"] = protocol_mod
