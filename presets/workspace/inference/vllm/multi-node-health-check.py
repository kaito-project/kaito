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
import logging
import os
import sys


def liveness(args):
    from ray.util.state import list_actors, list_jobs

    gcs = f"{args.leader_address}:{args.ray_port}"

    # Checking the Ray job's entrypoint is the best we can do to identify the VLLM inference job
    inference_job = next(
        filter(
            lambda job: job.entrypoint.startswith(
                "python3 /workspace/vllm/inference_api.py"
            ),
            list_jobs(address=gcs),
        ),
        None,
    )
    assert inference_job is not None, "Inference job not found in Ray jobs"

    has_dead_actors = False
    for actor in list_actors(
        address=gcs,
        filters=[("job_id", "=", inference_job.job_id), ("state", "!=", "ALIVE")],
    ):
        logging.error(
            f"Ray actor {actor.actor_id} is {actor.state}: {actor.death_cause}"
        )
        has_dead_actors = True

    if has_dead_actors:
        sys.exit(1)


def readiness(args):
    import requests

    vllm_health_endpoint = f"http://{args.leader_address}:{args.vllm_port}/health"
    try:
        response = requests.get(vllm_health_endpoint)
        if response.status_code != 200:
            sys.exit(1)
    except requests.exceptions.RequestException as e:
        print(f'Get "{vllm_health_endpoint}": {e}')
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Health check for VLLM multi-node setup"
    )
    parser.add_argument(
        "probe",
        type=str,
        choices=["liveness", "readiness"],
        help="Type of health check probe",
    )
    parser.add_argument(
        "--leader-address",
        type=str,
        required=True,
        help="Leader address of the Ray cluster",
    )
    parser.add_argument(
        "--ray-port", type=int, default=6379, help="Ray port of the cluster"
    )
    parser.add_argument(
        "--vllm-port", type=int, default=5000, help="VLLM port for the API server"
    )
    args = parser.parse_args()

    is_leader = os.environ.get("POD_INDEX") == "0"

    # only run liveness probe on the leader node because restarting workers would require
    # restarting the leader node as well. In other words, we are delegating the worker's
    # liveness check to the leader node.
    if args.probe == "liveness" and is_leader:
        assert args.ray_port is not None, (
            "Ray port must be specified for liveness probe"
        )
        liveness(args)
    elif args.probe == "readiness":
        assert args.vllm_port is not None, (
            "VLLM port must be specified for readiness probe"
        )
        readiness(args)
