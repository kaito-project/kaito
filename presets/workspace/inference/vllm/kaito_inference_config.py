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

from dataclasses import dataclass
from typing import Any

import yaml


@dataclass
class KaitoInferenceConfig:
    """Shared inference_config.yaml settings used by vLLM and LMCache."""

    vllm: dict[str, Any]
    max_probe_steps: int
    kv_cache_cpu_memory_utilization: float

    @staticmethod
    def from_yaml(yaml_file: str) -> "KaitoInferenceConfig":
        with open(yaml_file) as file:
            config_data = yaml.safe_load(file) or {}
        return KaitoInferenceConfig(
            vllm=config_data.get("vllm", {}),
            max_probe_steps=config_data.get("max_probe_steps", 6),
            kv_cache_cpu_memory_utilization=config_data.get(
                "kv_cache_cpu_memory_utilization", 0.5
            ),
        )

    def to_yaml(self) -> str:
        return yaml.dump(self.__dict__)
