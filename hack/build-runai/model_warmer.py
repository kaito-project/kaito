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

import os
import re
import socket
import time
from datetime import UTC, datetime

_CACHE_CONNECT_TIMEOUT_SECONDS = 2
_CACHE_RETRY_INTERVAL_SECONDS = 5
_CACHE_LOG_INTERVAL_ATTEMPTS = 6
_PARTITION_COUNT_ENV = "KAITO_MODEL_WARMER_PARTITION_COUNT"


def log(message):
    print(
        f"{datetime.now(UTC).isoformat()} [dacs-warmer] {message}",
        flush=True,
    )


def remain_alive():
    while True:
        time.sleep(3600)


def wait_for_cache():
    endpoint = os.environ["CACHE_DISCOVERY_URL"]
    port = int(os.environ["CACHE_SERVER_PORT"])
    attempt = 0

    log(f"waiting for cache endpoint {endpoint}:{port}")
    while True:
        attempt += 1
        try:
            with socket.create_connection(
                (endpoint, port),
                timeout=_CACHE_CONNECT_TIMEOUT_SECONDS,
            ):
                log(f"cache endpoint is reachable {endpoint}:{port}")
                return
        except OSError as error:
            if (attempt - 1) % _CACHE_LOG_INTERVAL_ATTEMPTS == 0:
                log(
                    f"cache endpoint is not reachable {endpoint}:{port}; "
                    f"attempt={attempt} retrying in "
                    f"{_CACHE_RETRY_INTERVAL_SECONDS}s: {error!r}"
                )
            time.sleep(_CACHE_RETRY_INTERVAL_SECONDS)


def select_weight_files(weight_files, partition_index=0, partition_count=1):
    if partition_count < 1:
        raise ValueError(f"partition count must be positive: {partition_count}")
    if partition_index < 0 or partition_index >= partition_count:
        raise ValueError(
            f"partition index {partition_index} is outside [0, {partition_count})"
        )
    return sorted(weight_files)[partition_index::partition_count]


def warmer_partition(pod_name):
    ordinal_match = re.search(r"-(\d+)$", pod_name)
    if ordinal_match is None:
        return None

    ordinal = int(ordinal_match.group(1))
    partition_count_value = os.environ.get(_PARTITION_COUNT_ENV)
    if partition_count_value is None:
        return (0, 1) if ordinal == 0 else None

    partition_count = int(partition_count_value)
    if partition_count < 1:
        raise ValueError(f"partition count must be positive: {partition_count}")
    if ordinal >= partition_count:
        raise ValueError(
            f"pod ordinal {ordinal} is outside configured partition count "
            f"{partition_count}"
        )
    return ordinal, partition_count


def warm_model(model_path, partition_index=0, partition_count=1):
    from runai_model_streamer import SafetensorsStreamer, list_safetensors

    weight_files = list_safetensors(path=model_path)
    if not weight_files:
        raise RuntimeError(f"no safetensors files discovered at {model_path}")
    assigned_files = select_weight_files(
        weight_files,
        partition_index,
        partition_count,
    )
    log(
        f"discovered files={len(weight_files)} "
        f"partition={partition_index}/{partition_count} "
        f"assigned_files={len(assigned_files)}"
    )
    if not assigned_files:
        log(f"partition={partition_index}/{partition_count} has no files")
        return

    started = time.monotonic()
    last_progress = started
    tensor_count = 0
    with SafetensorsStreamer() as streamer:
        streamer.stream_files(
            assigned_files,
            device="cpu",
            is_distributed=False,
        )
        total_tensors = sum(
            len(tensors_metadata)
            for tensors_metadata in streamer.files_to_tensors_metadata.values()
        )
        for _, tensor in streamer.get_tensors():
            tensor_count += 1
            del tensor
            now = time.monotonic()
            if now - last_progress >= 10:
                log(
                    f"progress tensors={tensor_count}/{total_tensors} "
                    f"elapsed_seconds={now - started:.2f}"
                )
                last_progress = now

    elapsed = time.monotonic() - started
    log(
        f"completed tensors={tensor_count} files={len(assigned_files)} "
        f"partition={partition_index}/{partition_count} "
        f"elapsed_seconds={elapsed:.2f}"
    )


def main():
    pod_name = os.environ.get("POD_NAME", "")
    try:
        partition = warmer_partition(pod_name)
    except ValueError as error:
        log(f"warm failed; invalid partition configuration: {error}")
        remain_alive()

    if partition is None:
        if re.search(r"-(\d+)$", pod_name):
            log("no-op: partitioning is disabled and pod ordinal is greater than zero")
        else:
            log(f"no-op: pod name has no StatefulSet ordinal: {pod_name}")
        remain_alive()

    try:
        partition_index, partition_count = partition
        model_path = os.environ["KAITO_MODEL_PATH"].rstrip("/")
        wait_for_cache()
        log(
            f"starting model stream for {model_path} "
            f"partition={partition_index}/{partition_count}"
        )
        warm_model(model_path, partition_index, partition_count)
        log("remaining alive after successful warm")
    except Exception as error:
        log(f"warm failed; vLLM will continue without prewarming: {error!r}")

    remain_alive()


if __name__ == "__main__":
    main()
