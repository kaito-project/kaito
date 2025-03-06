# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import os
from dataclasses import asdict, dataclass, field
from io import BytesIO
from typing import List, Optional

import diffusers
import GPUtil
import torch
import uvicorn
from diffusers import DiffusionPipeline
from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from transformers import HfArgumentParser


@dataclass
class DiffusionPipelineParams:
    """
    Diffusion Pipeline Configuration Parameters
    """
    pretrained_model_name_or_path: Optional[str] = field(default="/workspace/tfs/weights", metadata={"help": "Path to the pretrained model or model identifier from huggingface.co/models"})
    torch_dtype: Optional[str] = field(default=None, metadata={"help": "The torch dtype for the pre-trained model"})
    force_download: bool = field(default=False, metadata={"help": "Force the download of the model"})
    cache_dir: Optional[str] = field(default=None, metadata={"help": "Cache directory for the model"})
    resume_download: bool = field(default=False, metadata={"help": "Resume an interrupted download"})
    proxies: Optional[str] = field(default=None, metadata={"help": "Proxy configuration for downloading the model"})
    output_loading_info: bool = field(default=False, metadata={"help": "Output additional loading information"})
    allow_remote_files: bool = field(default=False, metadata={"help": "Allow using remote files, default is local only"})
    revision: str = field(default="main", metadata={"help": "Specific model version to use"})
    device_map: str = field(default="auto", metadata={"help": "The device map for the pre-trained model"})
    low_cpu_mem_usage: bool = field(default=True, metadata={"help": "Whether to use low CPU mem usage"})
    use_safetensors: bool = field(default=False, metadata={"help": "Whether to use safetensors"})
    use_onnx: bool = field(default=False, metadata={"help": "Whether to use ONNX"})
    
    # Method to process additional arguments
    def process_additional_args(self, addt_args: List[str]):
        """
        Process additional cmd line args and update the model configuration accordingly.
        """
        addt_args_dict = {}
        i = 0
        while i < len(addt_args):
            key = addt_args[i].lstrip('-')  # Remove leading dashes
            if i + 1 < len(addt_args) and not addt_args[i + 1].startswith('--'):
                value = addt_args[i + 1]
                i += 2  # Move past the current key-value pair
            else:
                value = True  # Assign a True value for standalone flags
                i += 1  # Move to the next item
            
            addt_args_dict[key] = value

        # Update the instance with the additional args
        self.__dict__.update(addt_args_dict)

    def __post_init__(self):
        """
        Post-initialization to validate some DiffusionPipelineParams values
        """
        if self.torch_dtype and not hasattr(torch, self.torch_dtype):
            raise ValueError(f"Invalid torch dtype: {self.torch_dtype}")
        self.torch_dtype = getattr(torch, self.torch_dtype) if self.torch_dtype else None

def get_scheduler_class(scheduler_name):
    try:
        if scheduler_name == "default": 
            return None
        # Dynamically get the class from the diffusers module
        scheduler_class = getattr(diffusers, scheduler_name)
    except AttributeError:
        raise ValueError(f"Scheduler {scheduler_name} either doesn't exist or isn't supported in the diffusers library.")
    return scheduler_class

# Captures users choice of scheduler
@dataclass
class SchedulerParams:
    scheduler_name: str = field(default="default", metadata={"help": "Type of scheduler to use"})

parser = HfArgumentParser((DiffusionPipelineParams, SchedulerParams))
pipeline_params, scheduler_params, additional_args = parser.parse_args_into_dataclasses(
    return_remaining_strings=True
)

# Load Pipeline
pipeline_params.process_additional_args(additional_args)
pipeline_args = asdict(pipeline_params)
pipeline_args["local_files_only"] = not pipeline_args.pop('allow_remote_files')
pipeline = DiffusionPipeline.from_pretrained(**pipeline_args)
# Check if CUDA is available and move the pipeline to CUDA if it is
if torch.cuda.is_available():
    pipeline = pipeline.to("cuda")

# Load Scheduler
SchedulerParamsClass = get_scheduler_class(scheduler_params.scheduler_name)
if SchedulerParamsClass:
    assert SchedulerParamsClass in pipeline.scheduler.comptaibles, f"Specified scheduler {scheduler_params.scheduler_name} isn't compatible with this diffusion pipeline"
    pipeline.scheduler = SchedulerParamsClass.from_config(pipeline.scheduler.config)

app = FastAPI()

@app.get('/')
def home():
    return "Server is running", 200

@app.get("/healthz")
def health_check():
    if not pipeline:
        raise HTTPException(status_code=500, detail="Pipeline not initialized")
    return {"status": "Healthy"}

@app.post("/generate")
def generate_image(prompt: str):
    try:
        image = pipeline(prompt).images[0]  # Generate the image

        img_io = BytesIO()
        image.save(img_io, 'PNG')  # Convert the PIL Image to bytes in PNG format
        img_io.seek(0)  # Rewind the buffer

        return StreamingResponse(img_io, media_type="image/png")  # Return the image as a streaming response
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/metrics")
def get_metrics():
    try:
        gpus = GPUtil.getGPUs()
        gpu_info = []
        for gpu in gpus:
            gpu_info.append({
                "id": gpu.id,
                "name": gpu.name,
                "load": f"{gpu.load * 100:.2f}%",  # Format as percentage
                "temperature": f"{gpu.temperature} C",
                "memory": {
                    "used": f"{gpu.memoryUsed / 1024:.2f} GB",
                    "total": f"{gpu.memoryTotal / 1024:.2f} GB"
                }
            })
        return {"gpu_info": gpu_info}
    except Exception as e:
        return {"error": str(e)}

if __name__ == "__main__":
    local_rank = int(os.environ.get("LOCAL_RANK", 0)) # Default to 0 if not set
    port = 5000 + local_rank # Adjust port based on local rank
    uvicorn.run(app=app, host='0.0.0.0', port=port)
