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
            "entrypoints.launchers",
            "entrypoints.launchers.api_server",
            "entrypoints.launchers.api_server.entry",
            "entrypoints.launchers.cli_args",
            "entrypoints.openai",
            "entrypoints.openai.models",
            "entrypoints.openai.models.protocol",
            "utils",
            "utils.argparse_utils",
            "v1",
            "v1.metrics",
            "v1.metrics.prometheus",
            "config",
            "config.lora",
            "lora",
            "lora.peft_helper",
        ],
    )

    # Mock other heavy deps that inference_api imports at module level.
    for mod in ("torch", "uvloop", "psutil"):
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

    # Stubs for the LoRA sizing helpers, mirroring vllm/config/lora.py and the
    # PEFTHelper validation in vllm/lora/peft_helper.py (vLLM 0.32.0). CI
    # installs vLLM, so there the tests exercise the real implementations.
    from dataclasses import dataclass, field
    from typing import Literal

    lora_config_mod = types.ModuleType("vllm.config.lora")
    lora_config_mod.MaxLoRARanks = Literal[1, 8, 16, 32, 64, 128, 256, 320, 512]
    sys.modules["vllm.config.lora"] = lora_config_mod

    @dataclass
    class PEFTHelper:
        r: int
        lora_alpha: int
        target_modules: list[str] | str
        rank_pattern: dict[str, int] = field(default_factory=dict)

        def __post_init__(self):
            if self.r <= 0:
                raise ValueError(
                    f"LoRA rank `r` must be a positive integer, got {self.r}."
                )
            self.rank_pattern = self.rank_pattern or {}
            for key, rank in self.rank_pattern.items():
                if not isinstance(rank, int) or rank <= 0:
                    raise ValueError(
                        f"LoRA rank for `{key}` must be a positive integer."
                    )
            self.vllm_lora_scaling_factor = self.lora_alpha / self.r

        @classmethod
        def from_dict(cls, config_dict: dict) -> "PEFTHelper":
            missing = {"r", "lora_alpha", "target_modules"} - set(config_dict)
            if missing:
                raise ValueError(f"Missing required configuration fields: {missing}")
            names = {"r", "lora_alpha", "target_modules", "rank_pattern"}
            return cls(**{k: v for k, v in config_dict.items() if k in names})

    peft_helper_mod = types.ModuleType("vllm.lora.peft_helper")
    peft_helper_mod.PEFTHelper = PEFTHelper
    sys.modules["vllm.lora.peft_helper"] = peft_helper_mod
else:
    # Some test modules stub vllm submodules such as vllm.v1 in sys.modules,
    # which breaks later imports of real vllm modules that depend on them.
    # Load the real modules inference_api needs before any test is collected.
    import vllm.config.lora  # noqa: F401
    import vllm.lora.peft_helper  # noqa: F401
