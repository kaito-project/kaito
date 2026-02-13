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

"""
KAITO inference server wrapping the HuggingFace ``transformers serve``
OpenAI-compatible engine.

The model (with any LoRA adapters merged) is loaded by KAITO and injected
into a ``ServeCommand`` instance so the serve engine uses it directly
without downloading or loading its own copy.

Endpoints exposed:
    POST /v1/chat/completions  – OpenAI-compatible chat completions (SSE)
    GET  /v1/models            – list the served model
    GET  /health               – liveness / readiness probe
    GET  /metrics              – GPU / CPU utilisation
"""

import codecs
import datetime
import logging
import os
import signal
import sys
import threading
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

import GPUtil
import psutil
import torch
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse, StreamingResponse
from peft import PeftModel
from pydantic import BaseModel
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    HfArgumentParser,
)
from transformers.commands.serving import ServeArguments, ServeCommand, TimedModel

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get("DEBUG_MODE", "false").lower() == "true"
logging.basicConfig(
    level=logging.DEBUG if debug_mode else logging.INFO,
    format="%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s",
    datefmt="%m-%d %H:%M:%S",
)

ADAPTERS_DIR = "/mnt/adapter"
# Large timeout so the TimedModel never auto-deletes the model;
# KAITO manages model lifecycle externally via pod lifecycle.
_MODEL_TIMEOUT_SECONDS = 315360000  # ~10 years


# ---------------------------------------------------------------------------
# Model configuration
# ---------------------------------------------------------------------------


@dataclass
class ModelConfig:
    """
    Transformers Model Configuration Parameters
    """

    pipeline: str | None = field(
        default="text-generation",
        metadata={"help": "The model pipeline for the pre-trained model"},
    )
    pretrained_model_name_or_path: str | None = field(
        default="/workspace/tfs/weights",
        metadata={
            "help": "Path to the pretrained model or model identifier from huggingface.co/models"
        },
    )
    combination_type: str | None = field(
        default="svd", metadata={"help": "The combination type of multi adapters"}
    )
    state_dict: dict[str, Any] | None = field(
        default=None, metadata={"help": "State dictionary for the model"}
    )
    cache_dir: str | None = field(
        default=None, metadata={"help": "Cache directory for the model"}
    )
    from_tf: bool = field(
        default=False, metadata={"help": "Load model from a TensorFlow checkpoint"}
    )
    force_download: bool = field(
        default=False, metadata={"help": "Force the download of the model"}
    )
    resume_download: bool = field(
        default=False, metadata={"help": "Resume an interrupted download"}
    )
    proxies: str | None = field(
        default=None, metadata={"help": "Proxy configuration for downloading the model"}
    )
    output_loading_info: bool = field(
        default=False, metadata={"help": "Output additional loading information"}
    )
    allow_remote_files: bool = field(
        default=False,
        metadata={"help": "Allow using remote files, default is local only"},
    )
    revision: str = field(
        default="main", metadata={"help": "Specific model version to use"}
    )
    trust_remote_code: bool = field(
        default=False,
        metadata={"help": "Enable trusting remote code when loading the model"},
    )
    load_in_4bit: bool = field(
        default=False, metadata={"help": "Load model in 4-bit mode"}
    )
    load_in_8bit: bool = field(
        default=False, metadata={"help": "Load model in 8-bit mode"}
    )
    torch_dtype: str | None = field(
        default=None, metadata={"help": "The torch dtype for the pre-trained model"}
    )
    device_map: str = field(
        default="auto", metadata={"help": "The device map for the pre-trained model"}
    )
    chat_template: str | None = field(
        default=None,
        metadata={
            "help": "The file path to the chat template, or the template in single-line form for the specified model"
        },
    )

    # Method to process additional arguments
    def process_additional_args(self, addt_args: list[str]):
        """
        Process additional cmd line args and update the model configuration accordingly.
        """
        addt_args_dict = {}
        i = 0
        while i < len(addt_args):
            key = addt_args[i].lstrip("-")  # Remove leading dashes
            if i + 1 < len(addt_args) and not addt_args[i + 1].startswith("--"):
                value = addt_args[i + 1]
                i += 2  # Move past the current key-value pair
            else:
                value = True  # Assign a True value for standalone flags
                i += 1  # Move to the next item

            addt_args_dict[key] = value

        # Update the ModelConfig instance with the additional args
        self.__dict__.update(addt_args_dict)

    def __post_init__(self):  # validate parameters
        """
        Post-initialization to validate some ModelConfig values
        """
        if self.torch_dtype == "auto":
            pass
        elif (
            self.torch_dtype
            and self.torch_dtype != "auto"
            and not hasattr(torch, self.torch_dtype)
        ):
            raise ValueError(f"Invalid torch dtype: {self.torch_dtype}")
        else:
            self.torch_dtype = (
                getattr(torch, self.torch_dtype) if self.torch_dtype else None
            )

        supported_pipelines = {"conversational", "text-generation"}
        if self.pipeline not in supported_pipelines:
            raise ValueError(f"Unsupported pipeline: {self.pipeline}")


# ---------------------------------------------------------------------------
# Chat template helper
# ---------------------------------------------------------------------------


def load_chat_template(chat_template: str | None) -> str | None:
    """Load a chat template from a file path or inline string."""
    if chat_template is None:
        return None

    if "{" in chat_template or "\n" in chat_template:
        resolved_chat_template = codecs.decode(chat_template, "unicode_escape")
    else:
        resolved_chat_template = Path(chat_template).read_text()

    logger.info("Chat template loaded successfully")
    logger.debug("Chat template: %s", resolved_chat_template)
    return resolved_chat_template


# ---------------------------------------------------------------------------
# Model & adapter loading
# ---------------------------------------------------------------------------

parser = HfArgumentParser(ModelConfig)
args, additional_args = parser.parse_args_into_dataclasses(
    return_remaining_strings=True
)
args.process_additional_args(additional_args)

model_args = asdict(args)
model_args["local_files_only"] = not model_args.pop("allow_remote_files")
model_args.pop("pipeline")  # Only used for validation in __post_init__
combination_type = model_args.pop("combination_type")

resolved_chat_template = load_chat_template(model_args.pop("chat_template"))
tokenizer = AutoTokenizer.from_pretrained(**model_args)
if resolved_chat_template is not None:
    tokenizer.chat_template = resolved_chat_template
base_model = AutoModelForCausalLM.from_pretrained(**model_args)

if not os.path.exists(ADAPTERS_DIR):
    logger.info("No adapters directory found, skipping adapter loading")
    model = base_model
else:
    valid_adapters_list = [
        os.path.join(ADAPTERS_DIR, d)
        for d in os.listdir(ADAPTERS_DIR)
        if os.path.isdir(os.path.join(ADAPTERS_DIR, d))
        and os.path.isfile(os.path.join(ADAPTERS_DIR, d, "adapter_config.json"))
    ]

    if len(valid_adapters_list) > 0:
        weights = []
        for adapter_path in valid_adapters_list:
            adapter_name = os.path.basename(adapter_path)
            weights.append(float(os.getenv(adapter_name, "1.0")))

        first_adapter = valid_adapters_list.pop(0)
        first_adapter_name = os.path.basename(first_adapter)
        model = PeftModel.from_pretrained(
            base_model, first_adapter, adapter_name=first_adapter_name
        )
        logger.info(f"First adapter loaded: {first_adapter}")

        for adapter_path in valid_adapters_list:
            adapter_name = os.path.basename(adapter_path)
            model.load_adapter(adapter_path, adapter_name=adapter_name)
            logger.info(f"Loaded adapter: {adapter_path}")

        adapter_names = [first_adapter_name] + [
            os.path.basename(p) for p in valid_adapters_list
        ]
        model.add_weighted_adapter(
            adapter_names,
            weights,
            combination_type=combination_type,
            adapter_name="combined_adapter",
        )
        model.set_adapter("combined_adapter")

        # Clean up individual adapters
        for name in adapter_names:
            model.delete_adapter(name)

        # Verify only combined adapter is active
        active = model.active_adapters
        assert active == ["combined_adapter"], (
            f"Expected ['combined_adapter'], got {active}"
        )

        logger.info("All adapters merged into 'combined_adapter'")
    else:
        logger.info("No valid adapters found, using base model")
        model = base_model

logger.info("Model loaded successfully")
logger.info("Model: %s", model)

# ---------------------------------------------------------------------------
# Set up transformers serve engine with pre-loaded model
# ---------------------------------------------------------------------------

model_name = args.pretrained_model_name_or_path
model_key = f"{model_name}@{args.revision}"

serve_args = ServeArguments(
    host="0.0.0.0",
    port=5000,
    force_model=model_name,
    model_timeout=_MODEL_TIMEOUT_SECONDS,
    device="auto",
)
serve_command = ServeCommand(serve_args)
# Inject the pre-loaded model (with adapters already merged) so the serve
# engine uses it directly instead of downloading/loading its own copy.
timed_model = TimedModel(
    model, timeout_seconds=_MODEL_TIMEOUT_SECONDS, processor=tokenizer
)
# Replace the auto-started timer with a daemon variant so it doesn't block
# process shutdown (the original timer is non-daemon and starts in __init__).
timed_model._timer.cancel()
_daemon_timer = threading.Timer(_MODEL_TIMEOUT_SECONDS, timed_model.timeout_reached)
_daemon_timer.daemon = True
_daemon_timer.start()
timed_model._timer = _daemon_timer
serve_command.loaded_models[model_key] = timed_model

# ---------------------------------------------------------------------------
# FastAPI app with OpenAI-compatible + KAITO-specific endpoints
# ---------------------------------------------------------------------------

app = FastAPI()


@app.post("/v1/chat/completions")
def chat_completion(body: dict):
    """OpenAI-compatible chat completions endpoint (SSE streamed)."""
    serve_command.validate_chat_completion_request(request=body)
    output = serve_command.generate_chat_completion(body)
    return StreamingResponse(output, media_type="text/event-stream")


@app.get("/v1/models")
def list_models():
    """List the served model in OpenAI-compatible format."""
    return JSONResponse(
        {
            "object": "list",
            "data": [
                {
                    "id": model_name,
                    "object": "model",
                    "created": int(datetime.datetime.now().timestamp()),
                    "owned_by": "kaito",
                }
            ],
        }
    )


@app.get("/health")
def health_check():
    """Health check used by Kubernetes liveness/readiness probes."""
    if model_key not in serve_command.loaded_models:
        logger.error("Model not loaded in serve engine")
        raise HTTPException(status_code=500, detail="Model not initialized")
    if serve_command.loaded_models[model_key].is_deleted():
        logger.error("Model was deleted from serve engine")
        raise HTTPException(status_code=500, detail="Model not initialized")
    return {"status": "Healthy"}


# ---------------------------------------------------------------------------
# Metrics endpoint
# ---------------------------------------------------------------------------


class MemoryInfo(BaseModel):
    used: str
    total: str


class CPUInfo(BaseModel):
    load_percentage: float
    physical_cores: int
    total_cores: int
    memory: MemoryInfo


class GPUInfo(BaseModel):
    id: str
    name: str
    load: str
    temperature: str
    memory: MemoryInfo


class ErrorResponse(BaseModel):
    detail: str


class MetricsResponse(BaseModel):
    gpu_info: list[GPUInfo] | None = None
    cpu_info: CPUInfo | None = None


@app.get(
    "/metrics",
    response_model=MetricsResponse,
    summary="Metrics Endpoint",
    responses={
        200: {
            "description": "Successful Response",
            "content": {
                "application/json": {
                    "examples": {
                        "gpu_metrics": {
                            "summary": "Example when GPUs are available",
                            "value": {
                                "gpu_info": [
                                    {
                                        "id": "GPU-1234",
                                        "name": "GeForce GTX 950",
                                        "load": "25.00%",
                                        "temperature": "55 C",
                                        "memory": {
                                            "used": "1.00 GB",
                                            "total": "2.00 GB",
                                        },
                                    }
                                ],
                                "cpu_info": None,  # Indicates CPUs info might not be present when GPUs are available
                            },
                        },
                        "cpu_metrics": {
                            "summary": "Example when only CPU is available",
                            "value": {
                                "gpu_info": None,  # Indicates GPU info might not be present when only CPU is available
                                "cpu_info": {
                                    "load_percentage": 20.0,
                                    "physical_cores": 4,
                                    "total_cores": 8,
                                    "memory": {"used": "4.00 GB", "total": "16.00 GB"},
                                },
                            },
                        },
                    }
                }
            },
        },
        500: {
            "description": "Internal Server Error",
            "model": ErrorResponse,
        },
    },
)
def get_metrics():
    """
    Provides system metrics, including GPU details if available, or CPU and memory usage otherwise.
    Useful for monitoring the resource utilization of the server running the ML models.
    """
    try:
        if torch.cuda.is_available():
            gpus = GPUtil.getGPUs()
            gpu_info = [
                GPUInfo(
                    id=str(gpu.id),
                    name=gpu.name,
                    load=f"{gpu.load * 100:.2f}%",
                    temperature=f"{gpu.temperature} C",
                    memory=MemoryInfo(
                        used=f"{gpu.memoryUsed / (1024**3):.2f} GB",
                        total=f"{gpu.memoryTotal / (1024**3):.2f} GB",
                    ),
                )
                for gpu in gpus
            ]
            return MetricsResponse(gpu_info=gpu_info)
        else:
            # Gather CPU metrics
            cpu_usage = psutil.cpu_percent(interval=1, percpu=False)
            physical_cores = psutil.cpu_count(logical=False)
            total_cores = psutil.cpu_count(logical=True)
            virtual_memory = psutil.virtual_memory()
            memory = MemoryInfo(
                used=f"{virtual_memory.used / (1024**3):.2f} GB",
                total=f"{virtual_memory.total / (1024**3):.2f} GB",
            )
            cpu_info = CPUInfo(
                load_percentage=cpu_usage,
                physical_cores=physical_cores,
                total_cores=total_cores,
                memory=memory,
            )
            return MetricsResponse(cpu_info=cpu_info)
    except Exception as e:
        logger.error(f"Error fetching metrics: {e}")
        raise HTTPException(status_code=500, detail=str(e))


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------


def shutdown_handler(sig, frame):
    sys.exit(0)


if __name__ == "__main__":
    signal.signal(signal.SIGINT, shutdown_handler)
    local_rank = int(os.environ.get("LOCAL_RANK", 0))  # Default to 0 if not set
    port = 5000 + local_rank  # Adjust port based on local rank
    logger.info(f"Starting server on port {port}")
    uvicorn.run(app=app, host="0.0.0.0", port=port)
