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

import argparse
import csv
import time
import uuid
from datetime import datetime

import torch
import transformers
from accelerate import Accelerator
from transformers import AutoModelForCausalLM, AutoTokenizer


def get_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", type=str, required=True)
    parser.add_argument("--num_nodes", type=int, required=True)
    parser.add_argument("--num_processes", type=int, required=True)
    parser.add_argument("--num_gpus", type=int, required=True)
    parser.add_argument("--num_prompts", type=int, required=True)
    parser.add_argument("--model_parallelism", type=str, required=True)
    parser.add_argument("--data_parallelism", type=str, required=True)
    parser.add_argument("--quantization", type=str, required=True)
    parser.add_argument("--machine", type=str, required=True)
    parser.add_argument(
        "--use_accelerator",
        action="store_true",
        help="Use the Accelerator for parallel processing.",
    )
    return parser.parse_args()


def inference(requests):
    for request in requests:
        start_time = time.time()

        sequences = pipeline(
            request,
            max_length=200,
            do_sample=True,
            top_k=10,
            num_return_sequences=1,
            eos_token_id=tokenizer.eos_token_id,
        )

        end_time = time.time()
        inference_time = end_time - start_time
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

        result = {
            "model": args.model,
            "num_nodes": args.num_nodes,
            "num_processes": args.num_processes,
            "num_gpus": args.num_gpus,
            "num_prompts": args.num_prompts,
            "prompt_len": len(request),
            "model_parallelism": args.model_parallelism,
            "data_parallelism": args.data_parallelism,
            "quantization": args.quantization,
            "machine": args.machine,
            "inference_time": inference_time,
            "request_id": str(uuid.uuid4()),  # Generate a unique UUID
            "timestamp": timestamp,
        }
        writer.writerow(result)

        for seq in sequences:
            print(f"Result: {seq['generated_text']}")


args = get_args()

model_id = "tiiuae/falcon-7b-instruct"
tokenizer = AutoTokenizer.from_pretrained(model_id)
model = AutoModelForCausalLM.from_pretrained(
    model_id,
    device_map="auto",
    torch_dtype=torch.bfloat16,
    trust_remote_code=True,
)

pipeline = transformers.pipeline(
    "text-generation",
    model=model,
    tokenizer=tokenizer,
    torch_dtype=torch.bfloat16,
    trust_remote_code=True,
    device_map="auto",
)


with open("../common-gpt-questions.csv") as f:
    requests = [line.strip() for line in f.readlines()]

fieldnames = [
    "model",
    "num_nodes",
    "num_processes",
    "num_gpus",
    "num_prompts",
    "prompt_len",
    "model_parallelism",
    "data_parallelism",
    "quantization",
    "machine",
    "inference_time",
    "request_id",
    "timestamp",
]

with open("results.csv", "a", newline="") as f:
    writer = csv.DictWriter(f, fieldnames=fieldnames)
    writer.writeheader()

    if args.use_accelerator:
        accelerator = Accelerator()
        # Split requests across processes
        with accelerator.split_between_processes(requests) as split_requests:
            inference(split_requests)
    else:
        inference(requests)
