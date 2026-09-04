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

import importlib.metadata
import json

import torch
from runai_model_streamer import SafetensorsStreamer, list_safetensors

import runai_model_streamer_azure


def main() -> None:
    with SafetensorsStreamer():
        pass

    versions = {
        package: importlib.metadata.version(package)
        for package in (
            "runai-model-streamer",
            "runai-model-streamer-azure",
            "torch",
        )
    }
    versions["torch_cuda"] = torch.version.cuda
    versions["streamer"] = SafetensorsStreamer.__name__
    versions["list_safetensors"] = list_safetensors.__name__
    versions["azure_module"] = runai_model_streamer_azure.__name__
    print(json.dumps(versions, sort_keys=True))


if __name__ == "__main__":
    main()
