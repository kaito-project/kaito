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
import collections
import logging
import os
import socket
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

import psutil
import uvloop
import vllm.entrypoints.openai.api_server as api_server
import yaml
from huggingface_hub import HfFileSystem, scan_cache_dir
from prometheus_client import CONTENT_TYPE_LATEST, Gauge, generate_latest
from vllm.entrypoints.openai.models.protocol import LoRAModulePath
from vllm.utils.argparse_utils import FlexibleArgumentParser
from vllm.v1.metrics.prometheus import get_prometheus_registry

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get("DEBUG_MODE", "false").lower() == "true"
logging.basicConfig(
    level=logging.DEBUG if debug_mode else logging.INFO,
    format="%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s",
    datefmt="%m-%d %H:%M:%S",
)

# Prometheus metrics for model download monitoring.
# Explicitly registered in vLLM's prometheus registry so they appear at the
# /metrics endpoint served by vLLM's FastAPI app.
_registry = get_prometheus_registry()
kaito_model_download_speed_bytes_per_second = Gauge(
    "kaito_model_download_speed_bytes_per_second",
    "Current model download speed in bytes per second",
    registry=_registry,
)
kaito_model_download_remaining_seconds = Gauge(
    "kaito_model_download_remaining_seconds",
    "Estimated remaining time for model download in seconds; -1 when unknown",
    registry=_registry,
)


class KAITOArgumentParser(argparse.ArgumentParser):
    vllm_parser = FlexibleArgumentParser(description="vLLM serving server")

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)

        # Initialize vllm parser
        self.vllm_parser = api_server.make_arg_parser(self.vllm_parser)
        self._reset_vllm_defaults()

        # KAITO only args
        # They should start with "kaito-" prefix to avoid conflict with vllm args
        self.add_argument(
            "--kaito-adapters-dir",
            type=str,
            default="/mnt/adapter",
            help="Directory where adapters are stored in KAITO preset.",
        )
        self.add_argument(
            "--kaito-config-file",
            type=str,
            default="",
            help="Additional args for KAITO preset.",
        )
        self.add_argument(
            "--kaito-max-probe-steps",
            type=int,
            help="Maximum number of steps to find the max available seq len fitting in the GPU memory.",
        )
        # Default is applied after file-config merging in parse_args so the
        # YAML config can still override an unspecified CLI value.
        self.add_argument(
            "--kaito-kv-cache-cpu-memory-utilization",
            type=float,
            default=None,
            help="KV cache CPU memory utilization. Defaults to 0.5 when neither this flag nor the kaito config file set it.",
        )

    def _reset_vllm_defaults(self):
        local_rank = int(os.environ.get("LOCAL_RANK", 0))  # Default to 0 if not set
        port = 5000 + local_rank  # Adjust port based on local rank

        server_default_args = {
            "disable_frontend_multiprocessing": False,
            "port": port,
        }
        self.vllm_parser.set_defaults(**server_default_args)

        # See https://docs.vllm.ai/en/stable/serving/engine_args.html for more args
        engine_default_args = {
            "model": "/workspace/vllm/weights",
            "cpu_offload_gb": 0,
            "gpu_memory_utilization": get_max_gpu_memory_utilization(),
            "disable_log_stats": False,
            "uvicorn_log_level": "error",
        }
        self.vllm_parser.set_defaults(**engine_default_args)

    def parse_args(self, *args, **kwargs):
        args = super().parse_known_args(*args, **kwargs)
        kaito_args = args[0]
        runtime_args = args[1]  # Remaining args

        # Load KAITO config
        if kaito_args.kaito_config_file:
            file_config = KaitoConfig.from_yaml(kaito_args.kaito_config_file)
            if kaito_args.kaito_max_probe_steps is None:
                kaito_args.kaito_max_probe_steps = file_config.max_probe_steps
            if kaito_args.kaito_kv_cache_cpu_memory_utilization is None:
                kaito_args.kaito_kv_cache_cpu_memory_utilization = (
                    file_config.kv_cache_cpu_memory_utilization
                )

            for key, value in file_config.vllm.items():
                runtime_args.append(f"--{key}")
                runtime_args.append(str(value))

        # Apply CLI default only after file-config merging so the YAML can
        # override an unspecified CLI value.
        if kaito_args.kaito_kv_cache_cpu_memory_utilization is None:
            kaito_args.kaito_kv_cache_cpu_memory_utilization = 0.5

        vllm_args = self.vllm_parser.parse_args(runtime_args, **kwargs)
        # Merge KAITO and vLLM args
        return argparse.Namespace(**vars(kaito_args), **vars(vllm_args))

    def print_help(self, file=None):
        super().print_help(file)
        print("\norignal vLLM server arguments:\n")
        self.vllm_parser.print_help(file)


@dataclass
class KaitoConfig:
    # Extra arguments for the vllm serving server, will be forwarded to the vllm server.
    # This should be in key-value format.
    vllm: dict[str, Any]

    # Maximum number of steps to find the max available seq len fitting in the GPU memory.
    max_probe_steps: int

    # Optional: CPU memory utilization for the vllm engine in kv cache offload mode. (default: 0.5, set to 0 to disable)
    kv_cache_cpu_memory_utilization: float

    @staticmethod
    def from_yaml(yaml_file: str) -> "KaitoConfig":
        with open(yaml_file) as file:
            config_data = yaml.safe_load(file)
        return KaitoConfig(
            vllm=config_data.get("vllm", {}),
            max_probe_steps=config_data.get("max_probe_steps", 6),
            kv_cache_cpu_memory_utilization=config_data.get(
                "kv_cache_cpu_memory_utilization", 0.5
            ),
        )

    def to_yaml(self) -> str:
        return yaml.dump(self.__dict__)


def _model_repo_path(model_id: str | None, cache_dir: str) -> str | None:
    """Return the HF cache repo directory for *model_id*, or None if not present."""
    if not model_id:
        return None
    try:
        for repo in scan_cache_dir(cache_dir=cache_dir).repos:
            if repo.repo_id == model_id:
                return str(repo.repo_path)
    except Exception as exc:
        logger.debug("Could not determine cache path for %s: %s", model_id, exc)
    return None


def _model_cache_size_bytes(repo_path: str | None) -> int:
    """Return bytes currently on disk under *repo_path*.

    Walks the repo dir counting only non-symlink files so HF Hub's snapshots/
    symlinks into blobs/ are not double-counted.  In-progress ``*.incomplete``
    blobs in blobs/ are included.
    """
    if repo_path is None:
        return 0
    try:
        return sum(
            f.stat().st_size
            for f in Path(repo_path).rglob("*")
            if f.is_file() and not f.is_symlink()
        )
    except OSError:
        return 0


def _download_in_progress(repo_path: str | None) -> bool:
    """True when HF Hub has in-progress ``*.incomplete`` blobs under *repo_path*.

    HF Hub writes blobs to ``blobs/<hash>.incomplete`` and atomically renames
    them once a file finishes downloading, so the presence of any such file
    is the authoritative signal that a download is still running.
    """
    if repo_path is None:
        return False
    blobs_dir = Path(repo_path) / "blobs"
    if not blobs_dir.is_dir():
        return False
    try:
        return any(p.name.endswith(".incomplete") for p in blobs_dir.iterdir())
    except OSError:
        return False


def _hf_model_total_bytes(
    model_id: str,
    token: str | None = None,
) -> int | None:
    """Return the total size in bytes of all files in *model_id* on HuggingFace Hub.

    Uses HfFileSystem so the result reflects the actual remote file sizes
    regardless of what is already cached locally.
    Returns None when the size cannot be determined.
    """
    try:
        fs = HfFileSystem(token=token)
        entries = fs.ls(model_id, detail=True)
        total = sum(
            info["size"]
            for info in entries
            if info.get("type") == "file" and info.get("size")
        )
        logger.debug("Model %s remote size: %.2f GiB", model_id, total / 1024**3)
        return total if total > 0 else None
    except Exception as exc:
        logger.warning("Could not fetch remote size for %s: %s", model_id, exc)
        return None


class ModelDownloadMonitor:
    """Background daemon thread that tracks HuggingFace model download progress.

    Polls *watch_dir* every ``_SAMPLE_INTERVAL`` seconds, maintains a rolling
    average speed over ``_WINDOW_SIZE`` samples (~10 s), and updates the two
    module-level Prometheus gauges:
      - kaito_model_download_speed_bytes_per_second
      - kaito_model_download_remaining_seconds  (-1 when total size is unknown)
    """

    _SAMPLE_INTERVAL: float = 2.0
    _WINDOW_SIZE: int = 5  # rolling window → ~10 s of history

    def __init__(self, watch_dir: str) -> None:
        self._watch_dir = watch_dir
        self._stop = threading.Event()

    def start(self, model_id: str | None = None, hf_token: str | None = None) -> None:
        t = threading.Thread(
            target=self._run,
            args=(model_id, hf_token),
            daemon=True,
            name="kaito-download-monitor",
        )
        t.start()

    def stop(self) -> None:
        kaito_model_download_speed_bytes_per_second.set(0)
        kaito_model_download_remaining_seconds.set(0)
        self._stop.set()

    def _run(self, model_id: str | None, hf_token: str | None) -> None:
        # For local model paths nothing is downloaded; publish zero immediately.
        if not model_id or os.path.isabs(model_id):
            kaito_model_download_speed_bytes_per_second.set(0)
            kaito_model_download_remaining_seconds.set(0)
            return

        expected_bytes: int | None = None
        if model_id:
            expected_bytes = _hf_model_total_bytes(model_id, token=hf_token)
            if expected_bytes:
                logger.info(
                    "Expected model size: %.2f GiB",
                    expected_bytes / 1024**3,
                )

        samples: collections.deque[float] = collections.deque(maxlen=self._WINDOW_SIZE)
        repo_path = _model_repo_path(model_id, self._watch_dir)
        last_size = _model_cache_size_bytes(repo_path)
        start_ts = time.monotonic()
        last_ts = start_ts

        while not self._stop.wait(self._SAMPLE_INTERVAL):
            now = time.monotonic()
            # Re-resolve the repo path until it exists; HF creates it lazily
            # on first download.
            if repo_path is None:
                repo_path = _model_repo_path(model_id, self._watch_dir)
            current_size = _model_cache_size_bytes(repo_path)
            elapsed = now - last_ts

            if elapsed > 0:
                samples.append((current_size - last_size) / elapsed)

            avg_speed = sum(samples) / len(samples) if samples else 0.0
            kaito_model_download_speed_bytes_per_second.set(max(0.0, avg_speed))

            # Authoritative completion signal: HF Hub no longer has any
            # *.incomplete blobs. This avoids relying on expected_bytes,
            # which is over-counted when the repo ships duplicate weight
            # formats (e.g. *.bin AND *.safetensors) but vLLM only fetches
            # one set.
            if (
                repo_path is not None
                and current_size > 0
                and not _download_in_progress(repo_path)
            ):
                kaito_model_download_remaining_seconds.set(0)
                logger.info(
                    "Model %s download complete: %.2f GiB on disk in %.1fs",
                    model_id,
                    current_size / 1024**3,
                    now - start_ts,
                )
                break
            elif expected_bytes is not None:
                remaining_bytes = max(0, expected_bytes - current_size)
                if remaining_bytes == 0:
                    kaito_model_download_remaining_seconds.set(0)
                elif avg_speed > 0:
                    kaito_model_download_remaining_seconds.set(
                        remaining_bytes / avg_speed
                    )
                else:
                    kaito_model_download_remaining_seconds.set(-1)
            else:
                kaito_model_download_remaining_seconds.set(-1)

            last_size = current_size
            last_ts = now


class _PreDownloadMetricsServer:
    """Thread-based HTTP server that serves /metrics on vLLM's port during
    model download, before vLLM's own HTTP server is ready.

    Running in a dedicated thread — completely separate from vLLM's asyncio
    event loop — ensures Prometheus scrapes are answered even when the event
    loop is blocked by heavy engine initialisation (CUDA setup, weight loading).
    """

    def __init__(self, sock_dup: socket.socket) -> None:
        class _Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                if self.path.split("?", 1)[0] != "/metrics":
                    self.send_response(404)
                    self.send_header("Content-Length", "0")
                    self.send_header("Connection", "close")
                    self.end_headers()
                    return
                output = generate_latest(_registry)
                self.send_response(200)
                self.send_header("Content-Type", CONTENT_TYPE_LATEST)
                self.send_header("Content-Length", str(len(output)))
                self.send_header("Connection", "close")
                self.end_headers()
                self.wfile.write(output)

            def log_message(self, *args) -> None:
                pass  # suppress access logs

        # bind_and_activate=False → TCPServer creates a socket but does not
        # call bind()/listen().  We close that unused socket and swap in our
        # pre-bound dup'd fd so that HTTPServer accepts on the right port.
        self._httpd = HTTPServer(("", 0), _Handler, bind_and_activate=False)
        self._httpd.socket.close()
        self._httpd.socket = sock_dup

    def start(self) -> None:
        t = threading.Thread(
            target=self._httpd.serve_forever,
            daemon=True,
            name="kaito-pre-download-metrics",
        )
        t.start()

    def stop(self) -> None:
        self._httpd.shutdown()
        self._httpd.server_close()


def load_lora_adapters(adapters_dir: str) -> LoRAModulePath | None:
    lora_list: list[LoRAModulePath] = []

    if not os.path.exists(adapters_dir):
        return lora_list

    logger.info(f"Loading LoRA adapters from {adapters_dir}")
    for adapter in os.listdir(adapters_dir):
        adapter_path = os.path.join(adapters_dir, adapter)
        if os.path.isdir(adapter_path):
            lora_list.append(LoRAModulePath(adapter, adapter_path))

    return lora_list


# Snippet executed in a throwaway subprocess to read GPU memory.
# torch.cuda.mem_get_info reports per-slice memory correctly under MIG (unlike
# NVML/pynvml, which fails with NVMLError_NoPermission or reports the parent
# GPU), but the first CUDA call initializes a CUDA context that reserves
# ~200-400MB and cannot be freed until the process exits. Running it in a
# subprocess reclaims that memory immediately, instead of pinning it in the
# long-lived launcher process, which never touches CUDA again after this.
_GPU_MEM_INFO_SNIPPET = (
    "import sys, torch; "
    "free, total = torch.cuda.mem_get_info(int(sys.argv[1])); "
    "print(f'KAITO_MEM_INFO {free} {total}')"
)

# Fallback gpu_memory_utilization used when the GPU memory probe fails.
_DEFAULT_GPU_MEMORY_UTILIZATION = 0.84


def _query_gpu_mem_info(device_index: int) -> tuple[int, int]:
    """Return (free_bytes, total_bytes) for *device_index*.

    Delegates to a short-lived subprocess so the CUDA context that
    torch.cuda.mem_get_info creates is torn down on exit rather than lingering
    in the caller for the process lifetime.
    """
    result = subprocess.run(
        [sys.executable, "-c", _GPU_MEM_INFO_SNIPPET, str(device_index)],
        capture_output=True,
        text=True,
        timeout=120,
        check=True,
    )
    for line in result.stdout.splitlines():
        if line.startswith("KAITO_MEM_INFO"):
            _, free_str, total_str = line.split()
            return int(free_str), int(total_str)
    raise RuntimeError(f"unexpected GPU mem-info output: {result.stdout!r}")


def get_max_gpu_memory_utilization(device_index: int = 0) -> float:
    # Calculate gpu_memory_utilization based on available GPU memory.
    # This ensures vLLM only uses currently free memory to avoid OOM errors.
    # See https://github.com/kaito-project/kaito/issues/1374.
    try:
        free_bytes, total_bytes = _query_gpu_mem_info(device_index)
    except Exception as exc:
        # Never fail startup (or leave a CUDA context in this process) over a
        # best-effort measurement; fall back to a conservative default.
        logger.warning(
            "Could not query GPU memory (%s); falling back to gpu_memory_utilization=%s",
            exc,
            _DEFAULT_GPU_MEMORY_UTILIZATION,
        )
        return _DEFAULT_GPU_MEMORY_UTILIZATION

    # Reserve an additional 600MiB for pytorch memory fragments, calculated based on profiling
    free_memory = free_bytes - 600 * 1024**2

    # Floor to 2 decimal places
    gpu_memory_utilization = (free_memory * 100 // total_bytes) / 100

    # The value is capped at 0.95 to maintain compatibility with previous behavior
    gpu_memory_utilization = min(0.95, gpu_memory_utilization)

    logger.info(f"Set default gpu_memory_utilization to {gpu_memory_utilization}")
    return gpu_memory_utilization


def set_kv_transfer_config_if_applicable(args: argparse.Namespace) -> None:
    """
    Set KV transfer config and optionally enable KV cache offloading to CPU RAM.
    - When KAITO_INFERENCE_ROLE is set: use NixlConnector (kv_both + fail policy).
    - When kaito_kv_cache_cpu_memory_utilization is set: use LMCacheConnectorV1 with CPU offload.
    """
    # Configure kv_transfer_config for P/D disaggregation using NixlConnector.
    inference_role = os.environ.get("KAITO_INFERENCE_ROLE", "")
    if args.kv_transfer_config is None and inference_role in ("prefill", "decode"):
        args.kv_transfer_config = {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }

    if (
        args.kaito_kv_cache_cpu_memory_utilization is None
        or args.kaito_kv_cache_cpu_memory_utilization <= 0
    ):
        logger.info(
            "kv_cache_cpu_memory_utilization is not set, do not use KV cache offload to CPU RAM."
        )
        return

    os.environ["LMCACHE_CHUNK_SIZE"] = "256"
    os.environ["LMCACHE_LOCAL_CPU"] = "True"
    available_memory_gb = (
        psutil.virtual_memory().total - psutil.virtual_memory().used
    ) / (1024**3)
    logger.info(
        f"Offload KV cache to CPU RAM, size limit: {available_memory_gb} * {args.kaito_kv_cache_cpu_memory_utilization} GB split among {args.tensor_parallel_size} GPUs"
    )

    # When using tensor parallelism, the KV cache CPU memory allocation must be divided evenly
    # across all GPUs. Each GPU should only allocate its portion (1/tensor_parallel_size) of the
    # total available CPU memory to prevent OOM.
    os.environ["LMCACHE_MAX_LOCAL_CPU_SIZE"] = (
        f"{available_memory_gb * args.kaito_kv_cache_cpu_memory_utilization / args.tensor_parallel_size}"
    )

    # Default to LMCacheConnectorV1 when CPU offload is enabled but no kv_transfer_config set.
    if args.kv_transfer_config is None:
        args.kv_transfer_config = {
            "kv_connector": "LMCacheConnectorV1",
            "kv_role": "kv_both",
        }


if __name__ == "__main__":
    parser = KAITOArgumentParser(description="KAITO wrapper of vLLM serving server")
    args = parser.parse_args()

    # set LoRA adapters
    if args.lora_modules is None:
        args.lora_modules = load_lora_adapters(args.kaito_adapters_dir)

    set_kv_transfer_config_if_applicable(args)

    logger.info(f"Starting server on port {args.port}")

    # Always start the download monitor so both metrics are always exposed.
    # For local model paths _run returns 0 immediately; for HF repo IDs it
    # tracks bandwidth throughout the download.
    watch_dir = getattr(args, "download_dir", None) or os.path.join(
        os.environ.get("HF_HOME", os.path.expanduser("~/.cache/huggingface")),
        "hub",
    )
    monitor = ModelDownloadMonitor(watch_dir=watch_dir)
    monitor.start(model_id=args.model, hf_token=os.environ.get("HF_TOKEN"))

    if not os.path.isabs(args.model):
        # --model is a HuggingFace repo ID: vLLM downloads weights at startup.
        # We pre-bind vLLM's port and serve /metrics on it so Prometheus can
        # scrape download progress before vLLM's HTTP server is ready.
        #
        # Sequence:
        #   1. We bind args.port and dup() the socket.
        #   2. Thread-based metrics server accepts on the dup — completely
        #      independent of vLLM's asyncio event loop, so it responds even
        #      when the loop is blocked by CUDA init / weight loading.
        #   3. Patched setup_server returns the original socket to vLLM
        #      (no re-bind; port stays claimed throughout).
        #   4. vLLM downloads the model — metrics server still accepting.
        #   5. Patched build_and_serve stops the thread before uvicorn starts,
        #      zeros the gauges, and hands the port to vLLM.

        # Pre-bind the port vLLM will use.
        pre_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        pre_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        pre_sock.bind(("0.0.0.0", args.port))
        pre_sock.listen(getattr(args, "backlog", 2048))

        # Thread takes ownership of the dup'd fd; pre_sock is kept for
        # later transfer to vLLM's setup_server.
        pre_metrics = _PreDownloadMetricsServer(pre_sock.dup())
        pre_metrics.start()
        logger.info("Pre-download metrics server listening on port %d", args.port)

        # Return our already-bound socket so vLLM does not try to bind a
        # new one (which would fail with "address already in use").
        def _patched_setup(setup_args: argparse.Namespace):
            host = getattr(setup_args, "host", "0.0.0.0")
            return f"http://{host}:{setup_args.port}", pre_sock

        api_server.setup_server = _patched_setup

        # By the time build_and_serve runs, model downloading, KV cache
        # allocation, and model warmup are all complete. Stop the metrics
        # thread just before vLLM's app starts accepting connections, so
        # only one listener is active at a time.
        _orig_build_and_serve = api_server.build_and_serve

        async def _patched_build_and_serve(
            engine_client, listen_address, sock, bargs, **kw
        ):
            pre_metrics.stop()
            monitor.stop()
            logger.info(
                "Pre-download metrics server stopped; vLLM taking over port %d",
                args.port,
            )
            return await _orig_build_and_serve(
                engine_client, listen_address, sock, bargs, **kw
            )

        api_server.build_and_serve = _patched_build_and_serve

    # See https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html
    uvloop.run(api_server.run_server(args))
