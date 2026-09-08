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

"""CPU-only tests for the pre-engine, all-node auxiliary asset barrier."""

import argparse
import ast
import os
import sys
from pathlib import Path
from types import ModuleType
from unittest.mock import MagicMock, call

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import inference_api  # noqa: E402


def make_args(**overrides):
    values = {
        "model": "az://container/model",
        "tokenizer": None,
        "speculative_config": None,
        "distributed_executor_backend": "ray",
        "pipeline_parallel_size": 2,
        "tensor_parallel_size": 4,
        "kaito_model_asset_prefetch_timeout": 600,
    }
    values.update(overrides)
    return argparse.Namespace(**values)


def node(node_id, alive=True, gpus=4):
    return {"NodeID": node_id, "Alive": alive, "Resources": {"GPU": gpus}}


ALL_NON_WEIGHT = {
    "ignore_pattern": ["*.pt", "*.safetensors", "*.bin", "*.tensors", "*.pth"]
}
MODEL_ONLY = {"allow_pattern": ["*.model", "*.py", "*.json"]}


@pytest.mark.parametrize(
    "argv, timeout",
    [
        ([], 600),
        (
            [
                "--kaito-model-asset-prefetch-timeout=900",
            ],
            900,
        ),
    ],
)
def test_parser_accepts_prefetch_timeout(monkeypatch, argv, timeout):
    monkeypatch.setattr(
        inference_api.KAITOArgumentParser, "vllm_parser", argparse.ArgumentParser()
    )
    monkeypatch.setattr(
        inference_api.api_server, "make_arg_parser", lambda parser: parser
    )
    monkeypatch.setattr(inference_api, "get_max_gpu_memory_utilization", lambda: 0.9)
    args = inference_api.KAITOArgumentParser().parse_args(argv)
    assert args.kaito_model_asset_prefetch_timeout == timeout


@pytest.fixture
def runtime(monkeypatch):
    ray = MagicMock()
    ray.is_initialized.return_value = False
    ray.nodes.return_value = [node("leader"), node("worker"), node("dead", False)]
    context = ray.get_runtime_context.return_value
    context.runtime_env = {}
    context.get_node_id.return_value = "leader"
    strategy = MagicMock()
    monkeypatch.setitem(sys.modules, "ray", ray)
    scheduling = ModuleType("ray.util.scheduling_strategies")
    scheduling.NodeAffinitySchedulingStrategy = strategy
    monkeypatch.setitem(sys.modules, scheduling.__name__, scheduling)

    runai = ModuleType("vllm.transformers_utils.runai_utils")
    runai.is_runai_obj_uri = lambda uri: uri.lower().startswith(
        ("az://", "s3://", "gs://")
    )
    runai.ObjectStorageModel = MagicMock()
    monkeypatch.setitem(sys.modules, runai.__name__, runai)
    envs = ModuleType("vllm.envs")
    envs.VLLM_ASSETS_CACHE = "/cache/assets"
    monkeypatch.setattr(sys.modules["vllm"], "envs", envs, raising=False)
    monkeypatch.setitem(sys.modules, envs.__name__, envs)
    monkeypatch.delenv("VLLM_ASSETS_CACHE", raising=False)
    # Track writes to os.environ by the code under test for fixture cleanup.
    monkeypatch.setenv("VLLM_ASSETS_CACHE_MODEL_CLEAN", "1")
    yield ray, strategy, runai, envs
    os.environ.pop("VLLM_ASSETS_CACHE", None)


def test_prefetches_every_alive_node_with_hard_affinity(runtime):
    ray, strategy, _, _ = runtime
    inference_api.prefetch_model_assets(make_args())
    ray.init.assert_called_once_with(address="auto")
    ray.remote.assert_called_once_with(
        num_cpus=0, num_gpus=0, max_retries=0, max_calls=1
    )
    strategy.assert_has_calls([call("leader", soft=False), call("worker", soft=False)])
    download = ray.remote.return_value.return_value
    assert download.options.call_count == 2
    assert download.options.return_value.remote.call_args_list == [
        call({"az://container/model": ALL_NON_WEIGHT}),
        call({"az://container/model": ALL_NON_WEIGHT}),
    ]
    ray.get.assert_called_once()
    assert len(ray.get.call_args.args[0]) == 2
    assert 0 < ray.get.call_args.kwargs["timeout"] <= 600
    ray.cancel.assert_not_called()


@pytest.mark.parametrize(
    "overrides",
    [
        {"distributed_executor_backend": "mp"},
        {"distributed_executor_backend": None},
        {"model": "/local/model"},
        {"model": "org/model"},
    ],
)
def test_unaffected_launches_do_not_connect_ray(runtime, overrides):
    ray, _, _, _ = runtime
    inference_api.prefetch_model_assets(make_args(**overrides))
    ray.init.assert_not_called()
    ray.remote.assert_not_called()


@pytest.mark.parametrize("scheme", ["az", "s3", "gs"])
def test_uses_helper_supported_remote_schemes(runtime, scheme):
    ray, _, _, _ = runtime
    uri = f"{scheme}://container/exact-prefix/"
    args = make_args(model=uri)
    inference_api.prefetch_model_assets(args)
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {uri: ALL_NON_WEIGHT}
    )
    assert args.model == uri  # No normalization: the exact URI determines vLLM's hash.
    assert args.tokenizer is None


def test_separate_tokenizer_and_draft_assets(runtime):
    ray, _, _, _ = runtime
    inference_api.prefetch_model_assets(
        make_args(
            tokenizer="az://container/tokenizer",
            speculative_config={"model": "az://container/draft"},
        )
    )
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {
            "az://container/model": MODEL_ONLY,
            "az://container/tokenizer": ALL_NON_WEIGHT,
            "az://container/draft": ALL_NON_WEIGHT,
        }
    )


@pytest.mark.parametrize("speculative_config", [None, "az://container/draft", {}])
def test_unusable_speculative_config_is_ignored(runtime, speculative_config):
    """--speculative-config may arrive unparsed; never crash the launch on it."""
    ray, _, _, _ = runtime
    inference_api.prefetch_model_assets(
        make_args(speculative_config=speculative_config)
    )
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {"az://container/model": ALL_NON_WEIGHT}
    )


def test_missing_optional_vllm_args_do_not_crash(runtime):
    ray, _, _, _ = runtime
    args = make_args()
    del args.tokenizer
    del args.speculative_config
    inference_api.prefetch_model_assets(args)
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {"az://container/model": ALL_NON_WEIGHT}
    )


def test_remote_tokenizer_with_local_model(runtime):
    ray, _, _, _ = runtime
    inference_api.prefetch_model_assets(
        make_args(model="/model", tokenizer="az://container/tokenizer")
    )
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {"az://container/tokenizer": ALL_NON_WEIGHT}
    )


def test_local_tokenizer_does_not_download_remote_tokenizer_assets(runtime):
    ray, _, _, _ = runtime
    inference_api.prefetch_model_assets(make_args(tokenizer="/tokenizer"))
    ray.remote.return_value.return_value.options.return_value.remote.assert_called_with(
        {"az://container/model": MODEL_ONLY}
    )


def test_worker_reuses_vllm_helper_with_nonweight_filters(runtime):
    _, _, runai, _ = runtime
    model = MagicMock()
    tokenizer = MagicMock()
    runai.ObjectStorageModel.side_effect = [model, tokenizer]
    inference_api._prefetch_model_assets_on_node(
        {
            "az://container/model/": MODEL_ONLY,
            "az://container/tokenizer": ALL_NON_WEIGHT,
        }
    )
    assert runai.ObjectStorageModel.call_args_list == [
        call(url="az://container/model/"),
        call(url="az://container/tokenizer"),
    ]
    model.pull_files.assert_called_once_with(
        "az://container/model/", allow_pattern=["*.model", "*.py", "*.json"]
    )
    tokenizer.pull_files.assert_called_once_with(
        "az://container/tokenizer",
        ignore_pattern=["*.pt", "*.safetensors", "*.bin", "*.tensors", "*.pth"],
    )


def test_worker_propagates_download_failure(runtime):
    _, _, runai, _ = runtime
    runai.ObjectStorageModel.return_value.pull_files.side_effect = OSError("download")
    with pytest.raises(OSError, match="download"):
        inference_api._prefetch_model_assets_on_node({"az://container": ALL_NON_WEIGHT})


@pytest.mark.parametrize(
    "credential",
    ["AZURE_STORAGE_SAS_TOKEN", "AZURE_FEDERATED_TOKEN_FILE"],
)
def test_cache_and_runtime_env_preserved_without_copying_leader_credentials(
    runtime, monkeypatch, credential
):
    ray, _, _, envs = runtime
    ray.is_initialized.return_value = True
    context = ray.get_runtime_context.return_value
    context.runtime_env = {
        "working_dir": "s3://code/package.zip",
        "env_vars": {"USER_SETTING": "preserved"},
    }
    envs.VLLM_ASSETS_CACHE = "relative/assets"
    monkeypatch.setenv(credential, "leader-only")
    inference_api.prefetch_model_assets(make_args())
    ray.init.assert_not_called()
    options = ray.remote.return_value.return_value.options.call_args.kwargs
    assert options["runtime_env"] == {
        "working_dir": "s3://code/package.zip",
        "env_vars": {
            "USER_SETTING": "preserved",
            "VLLM_ASSETS_CACHE": os.path.abspath("relative/assets"),
            "VLLM_ASSETS_CACHE_MODEL_CLEAN": "0",
        },
    }
    assert os.environ["VLLM_ASSETS_CACHE"] == os.path.abspath("relative/assets")
    assert os.environ["VLLM_ASSETS_CACHE_MODEL_CLEAN"] == "1"
    assert context.runtime_env["env_vars"] == {"USER_SETTING": "preserved"}


@pytest.mark.parametrize("timeout", [0, -1])
def test_nonpositive_timeout_fails_before_connecting(runtime, timeout):
    ray, _, _, _ = runtime
    with pytest.raises(ValueError, match="must be positive"):
        inference_api.prefetch_model_assets(
            make_args(kaito_model_asset_prefetch_timeout=timeout)
        )
    ray.init.assert_not_called()


def test_missing_cluster_fails_without_starting_local_ray(runtime):
    ray, _, _, _ = runtime
    ray.init.side_effect = ConnectionError("missing cluster")
    with pytest.raises(ConnectionError, match="missing cluster"):
        inference_api.prefetch_model_assets(make_args())
    ray.init.assert_called_once_with(address="auto")
    ray.remote.assert_not_called()


def test_discovers_all_nodes_without_a_separate_cluster_size(runtime):
    ray, _, _, _ = runtime
    ray.nodes.return_value = [
        node("leader"),
        node("worker"),
        node("extra"),
        node("dead", False),
    ]
    inference_api.prefetch_model_assets(make_args())
    assert ray.remote.return_value.return_value.options.call_count == 3
    assert len(ray.get.call_args.args[0]) == 3


def test_single_node_ray_cluster_prefetches_without_count_argument(runtime):
    ray, _, _, _ = runtime
    ray.nodes.return_value = [node("leader")]
    inference_api.prefetch_model_assets(make_args(pipeline_parallel_size=1))
    assert ray.remote.return_value.return_value.options.call_count == 1


@pytest.mark.parametrize(
    "nodes",
    [[], [node("worker"), node("other")], [node("leader", alive=False)]],
)
def test_cluster_without_the_leader_fails(runtime, nodes):
    ray, _, _, _ = runtime
    ray.nodes.return_value = nodes
    with pytest.raises(RuntimeError, match="API leader"):
        inference_api.prefetch_model_assets(make_args())
    ray.remote.assert_not_called()


def test_gpu_topology_is_left_to_vllm(runtime):
    """vLLM only warns when GPUs look short; the prefetch must not hard-fail."""
    ray, _, _, _ = runtime
    ray.nodes.return_value = [node("leader", gpus=0), node("worker", gpus=0)]
    inference_api.prefetch_model_assets(make_args(tensor_parallel_size=8))
    assert ray.remote.return_value.return_value.options.call_count == 2


def test_driver_cache_setting_wins_and_is_pinned(runtime, monkeypatch):
    ray, _, _, envs = runtime
    envs.VLLM_ASSETS_CACHE = "/cache/assets"
    ray.get_runtime_context.return_value.runtime_env = {
        "env_vars": {"VLLM_ASSETS_CACHE": "/stale/assets"}
    }
    inference_api.prefetch_model_assets(make_args())
    options = ray.remote.return_value.return_value.options.call_args.kwargs
    assert options["runtime_env"]["env_vars"]["VLLM_ASSETS_CACHE"] == "/cache/assets"
    assert os.environ["VLLM_ASSETS_CACHE"] == "/cache/assets"


@pytest.mark.parametrize("error", [TimeoutError("prefetch"), RuntimeError("download")])
def test_barrier_failure_cancels_tasks_and_propagates(runtime, error):
    ray, _, _, _ = runtime
    ray.get.side_effect = error
    with pytest.raises(type(error), match=str(error)):
        inference_api.prefetch_model_assets(make_args())
    assert ray.cancel.call_count == 2
    assert all(c.kwargs == {"force": True} for c in ray.cancel.call_args_list)


def test_node_replacement_during_download_fails(runtime):
    ray, _, _, _ = runtime
    ray.nodes.side_effect = [
        [node("leader"), node("worker")],
        [node("leader"), node("replacement")],
    ]
    with pytest.raises(RuntimeError, match="membership changed"):
        inference_api.prefetch_model_assets(make_args())


def test_partial_submission_failure_cancels_started_tasks(runtime):
    ray, _, _, _ = runtime
    ray.remote.return_value.return_value.options.return_value.remote.side_effect = [
        "first-ref",
        RuntimeError("submission"),
    ]
    with pytest.raises(RuntimeError, match="submission"):
        inference_api.prefetch_model_assets(make_args())
    ray.cancel.assert_called_once_with("first-ref", force=True)


def test_cancellation_failure_does_not_hide_download_error(runtime):
    ray, _, _, _ = runtime
    ray.get.side_effect = RuntimeError("download")
    ray.cancel.side_effect = RuntimeError("cancellation")
    with pytest.raises(RuntimeError, match="download"):
        inference_api.prefetch_model_assets(make_args())


def test_prefetch_precedes_server_construction():
    tree = ast.parse(Path(inference_api.__file__).read_text())
    main = tree.body[-1]
    assert isinstance(main, ast.If)
    prefetch = next(
        statement
        for statement in main.body
        if isinstance(statement, ast.Expr)
        and isinstance(statement.value, ast.Call)
        and isinstance(statement.value.func, ast.Name)
        and statement.value.func.id == "prefetch_model_assets"
    )
    run_server = next(
        node
        for node in ast.walk(main)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and node.func.attr == "run_server"
    )
    assert prefetch.lineno < run_server.lineno
