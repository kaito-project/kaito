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

"""List all LLM model architectures supported by vLLM."""

import logging
import os

# Silence all loggers before any vllm import
logging.disable(logging.CRITICAL)
os.environ.setdefault("VLLM_LOGGING_LEVEL", "ERROR")
os.environ.setdefault("TRANSFORMERS_VERBOSITY", "error")
os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")


def list_from_installed():
    """Use the live ModelRegistry (requires vllm to be installed)."""
    from vllm.model_executor.models.registry import ModelRegistry

    archs = sorted(ModelRegistry.get_supported_archs())
    for arch in archs:
        print(arch)


if __name__ == "__main__":
    list_from_installed()
