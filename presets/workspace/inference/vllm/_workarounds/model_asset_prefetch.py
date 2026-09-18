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

"""Prefetch Run:ai object-storage model assets onto every Ray node.

TEMPORARY WORKAROUND. vLLM pulls a remote model's non-weight files only where
``ModelConfig`` is built and then hands the resolved node-local path to remote
workers without populating their filesystems, so multi-node workers start
against an empty directory. Delete this module and its three call sites in
inference_api.py (``import``, ``register_args``, ``prefetch_model_assets``) once
KAITO bundles the upstream fix: https://github.com/vllm-project/vllm/issues/50616
"""

import argparse
import logging
import os
import time
from typing import TypedDict

logger = logging.getLogger(__name__)


def register_args(parser: argparse.ArgumentParser) -> None:
    """Register the workaround's CLI flag on the KAITO argument parser."""
    parser.add_argument(
        "--kaito-model-asset-prefetch-timeout",
        type=int,
        default=600,
        help="Seconds allowed to prefetch model assets on the Ray cluster.",
    )


class PullPatterns(TypedDict, total=False):
    """Glob filters forwarded to ``ObjectStorageModel.pull_files``.

    Exactly one key is set per value: ``allow_pattern`` to whitelist a small set
    of files, or ``ignore_pattern`` to pull everything but the weights. ``total=
    False`` keeps the splat semantics of the previous bare dict so only the
    present key reaches ``pull_files``.
    """

    allow_pattern: list[str]
    ignore_pattern: list[str]


# Non-weight file patterns, mirroring
# vllm.config.model.ModelConfig.maybe_pull_model_tokenizer_for_runai so the
# prefetched cache is a superset of what the driver would resolve on its own.
_MODEL_ONLY_PATTERNS: PullPatterns = {"allow_pattern": ["*.model", "*.py", "*.json"]}
_ALL_NON_WEIGHT_PATTERNS: PullPatterns = {
    "ignore_pattern": ["*.pt", "*.safetensors", "*.bin", "*.tensors", "*.pth"]
}


def _prefetch_model_assets_on_node(assets: dict[str, PullPatterns]) -> None:
    """Mirror one node's Run:ai object-storage cache. Runs as a Ray task."""
    from vllm.transformers_utils.runai_utils import ObjectStorageModel

    for uri, patterns in assets.items():
        ObjectStorageModel(url=uri).pull_files(uri, **patterns)


def _remote_model_assets(args: argparse.Namespace) -> dict[str, PullPatterns]:
    """Map every remote URI vLLM will resolve locally to its pull_files patterns."""
    from vllm.transformers_utils.runai_utils import is_runai_obj_uri

    assets: dict[str, PullPatterns] = {}
    if is_runai_obj_uri(args.model):
        assets[args.model] = _MODEL_ONLY_PATTERNS
    # vLLM defaults the tokenizer to the model and then, for that case, re-pulls
    # the same directory with the wider non-weight filter.
    tokenizer = getattr(args, "tokenizer", None) or args.model
    if is_runai_obj_uri(tokenizer):
        assets[tokenizer] = _ALL_NON_WEIGHT_PATTERNS
    speculative_config = getattr(args, "speculative_config", None)
    if isinstance(speculative_config, dict):
        draft_model = speculative_config.get("model")
        # Heterogeneous-vocabulary drafts load their own tokenizer on workers.
        if isinstance(draft_model, str) and is_runai_obj_uri(draft_model):
            assets[draft_model] = _ALL_NON_WEIGHT_PATTERNS
    return assets


def prefetch_model_assets(args: argparse.Namespace) -> None:
    """Populate node-local Run:ai assets on every Ray node before serving.

    vLLM pulls a remote model's non-weight files (config, tokenizer, processor,
    ``trust_remote_code`` modules) only where ``ModelConfig`` is built, then ships
    the resolved *local* path to remote workers without populating their disks.
    KAITO runs this script on the Ray leader only, so multi-node workers would
    start against an empty directory. Mirror the cache onto every node first.

    The cache directory is derived from the URI alone
    (``$VLLM_ASSETS_CACHE/model_streamer/<sha256(uri)[:8]>``), so pulling the same
    URI on each node reproduces the exact path the driver hands to the workers.
    Weight files stay excluded: Run:ai streams those directly from object storage.
    """
    if args.distributed_executor_backend != "ray":
        return

    assets = _remote_model_assets(args)
    if not assets:
        return

    timeout = args.kaito_model_asset_prefetch_timeout
    if timeout <= 0:
        raise ValueError("--kaito-model-asset-prefetch-timeout must be positive")

    import ray
    from ray.util.scheduling_strategies import NodeAffinitySchedulingStrategy
    from vllm import envs

    deadline = time.monotonic() + timeout
    if not ray.is_initialized():
        # address="auto" attaches to the head started by multi-node-serving.sh and
        # raises if it is absent; it never silently forms a leader-only cluster.
        # Note: because the driver is now attached, vLLM's initialize_ray_cluster
        # takes its "already initialized" path and skips applying
        # parallel_config.ray_runtime_env. KAITO never sets that field; wire it in
        # below if it ever does.
        ray.init(address="auto")

    # Keep the driver's runtime environment (working_dir, pip, env_vars). Do not
    # add the leader's storage credentials: each pod's Ray runtime already
    # inherits its own from the entrypoint wrapper, and copying short-lived
    # leader tokens across nodes would only hide expiry bugs.
    runtime_env = dict(ray.get_runtime_context().runtime_env)
    worker_env = dict(runtime_env.get("env_vars", {}))
    # Pin one absolute cache root for the driver and every prefetch task so the
    # sha256 directory the driver resolves is the one the workers already have.
    cache_dir = os.path.abspath(os.path.expanduser(envs.VLLM_ASSETS_CACHE))
    os.environ["VLLM_ASSETS_CACHE"] = cache_dir
    worker_env["VLLM_ASSETS_CACHE"] = cache_dir
    # A prefetch task exiting must never take the assets down with it.
    worker_env["VLLM_ASSETS_CACHE_MODEL_CLEAN"] = "0"
    runtime_env["env_vars"] = worker_env

    # The leader startup script blocks until --ray_cluster_size nodes have joined
    # and the launch command only chains this process on success, so the alive set
    # here is the full cluster.
    nodes = [node["NodeID"] for node in ray.nodes() if node["Alive"]]
    if ray.get_runtime_context().get_node_id() not in nodes:
        raise RuntimeError("Ray cluster does not include the API leader")

    logger.info(
        "Prefetching model assets %s on %d Ray nodes", sorted(assets), len(nodes)
    )
    download = ray.remote(num_cpus=0, num_gpus=0, max_retries=0, max_calls=1)(
        _prefetch_model_assets_on_node
    )
    refs = []
    try:
        for node_id in nodes:
            refs.append(
                download.options(
                    scheduling_strategy=NodeAffinitySchedulingStrategy(
                        node_id, soft=False
                    ),
                    runtime_env=runtime_env,
                ).remote(assets)
            )
        ray.get(refs, timeout=max(0, deadline - time.monotonic()))
        if {node["NodeID"] for node in ray.nodes() if node["Alive"]} != set(nodes):
            raise RuntimeError("Ray cluster membership changed during asset prefetch")
    except BaseException:
        # A half-finished cache is worse than none: cancel the stragglers so a
        # restarted pod re-downloads instead of racing a stale writer.
        for ref in refs:
            try:
                ray.cancel(ref, force=True)
            except Exception:
                logger.warning("Failed to cancel asset-prefetch task", exc_info=True)
        raise
    logger.info("Model assets are ready on all %d Ray nodes", len(nodes))
