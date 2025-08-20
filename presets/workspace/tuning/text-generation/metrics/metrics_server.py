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


import logging
import os

import GPUtil
import psutil
import torch
import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get("DEBUG_MODE", "false").lower() == "true"
logging.basicConfig(level=logging.DEBUG if debug_mode else logging.INFO)

app = FastAPI()


class ErrorResponse(BaseModel):
    detail: str


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


if __name__ == "__main__":
    local_rank = int(os.environ.get("LOCAL_RANK", 0))  # Default to 0 if not set
    port = 5000 + local_rank  # Adjust port based on local rank
    logger.info(f"Starting server on port {port}")
    uvicorn.run(app=app, host="0.0.0.0", port=port)
