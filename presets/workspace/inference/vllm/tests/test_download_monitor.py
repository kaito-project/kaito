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

"""Unit tests for DownloadMonitor and related helpers in inference_api.py.

All heavy runtime dependencies (vllm, prometheus_client, pynvml, …) are
stubbed via sys.modules so these tests run on any machine, including Mac
dev environments without a GPU or vllm installed.
"""

import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

# ── Stub all dependencies that aren't available on a plain dev machine ───────
# Use setdefault so we don't replace already-loaded real modules when the
# full test suite runs in a GPU environment.
_HF_MOCK = MagicMock(name="huggingface_hub")
_STUBS = {
    "vllm": MagicMock(),
    "vllm.entrypoints": MagicMock(),
    "vllm.entrypoints.openai": MagicMock(),
    "vllm.entrypoints.openai.api_server": MagicMock(),
    "vllm.entrypoints.openai.models": MagicMock(),
    "vllm.entrypoints.openai.models.protocol": MagicMock(),
    "vllm.utils": MagicMock(),
    "vllm.utils.argparse_utils": MagicMock(),
    "vllm.v1": MagicMock(),
    "vllm.v1.metrics": MagicMock(),
    "vllm.v1.metrics.prometheus": MagicMock(),
    "torch": MagicMock(),
    "uvloop": MagicMock(),
    "psutil": MagicMock(),
    "prometheus_client": MagicMock(),
    "yaml": MagicMock(),
    "huggingface_hub": _HF_MOCK,
}
for _name, _mock in _STUBS.items():
    sys.modules.setdefault(_name, _mock)

_PARENT = str(Path(__file__).resolve().parent.parent)
if _PARENT not in sys.path:
    sys.path.insert(0, _PARENT)

import inference_api  # noqa: E402
from inference_api import (  # noqa: E402
    ModelDownloadMonitor,
    _download_in_progress,
    _hf_model_total_bytes,
    _model_cache_size_bytes,
    _model_repo_path,
)

# ── Helpers ──────────────────────────────────────────────────────────────────
MB = 1024**2


# ── _hf_model_total_bytes ─────────────────────────────────────────────────────
class TestHfModelTotalBytes:
    def _fs_entry(self, size, name="file.bin"):
        return {"type": "file", "name": f"org/model/{name}", "size": size}

    def _mock_fs(self, entries):
        """Return a mock HfFileSystem whose .ls() yields *entries*."""
        mock_fs_instance = MagicMock()
        mock_fs_instance.ls.return_value = entries
        mock_fs_cls = MagicMock(return_value=mock_fs_instance)
        return mock_fs_cls, mock_fs_instance

    def test_sums_file_sizes_ignoring_dirs_and_zero(self):
        entries = [
            self._fs_entry(1000, "config.json"),
            self._fs_entry(2000, "model.safetensors"),
            {"type": "directory", "name": "org/model/", "size": 0},  # skipped
            {"type": "file", "name": "org/model/empty", "size": 0},  # skipped (falsy)
        ]
        mock_cls, _ = self._mock_fs(entries)
        with patch.object(inference_api, "HfFileSystem", mock_cls):
            assert _hf_model_total_bytes("org/model") == 3000

    def test_returns_none_on_api_exception(self):
        mock_cls = MagicMock(side_effect=Exception("network error"))
        with patch.object(inference_api, "HfFileSystem", mock_cls):
            assert _hf_model_total_bytes("org/model") is None

    def test_returns_none_when_no_files(self):
        mock_cls, _ = self._mock_fs([])
        with patch.object(inference_api, "HfFileSystem", mock_cls):
            assert _hf_model_total_bytes("org/model") is None

    def test_passes_token_to_hffilesystem(self):
        mock_cls, mock_instance = self._mock_fs([self._fs_entry(500)])
        with patch.object(inference_api, "HfFileSystem", mock_cls):
            _hf_model_total_bytes("org/model", token="tok-abc")
        mock_cls.assert_called_once_with(token="tok-abc")
        mock_instance.ls.assert_called_once_with("org/model", detail=True)


# ── DownloadMonitor ───────────────────────────────────────────────────────────
class TestDownloadMonitor:
    """
    Each test drives DownloadMonitor._run() directly (no real thread) by
    mocking _stop.wait() to control the number of loop iterations, and
    patching _model_cache_size_bytes and time.monotonic for deterministic timing.
    """

    def _run_monitor(
        self,
        dir_sizes,
        timestamps,
        wait_returns,
        model_id=None,
        total_bytes=None,
        repo_path="/fake/weights/models--org--model",
        download_in_progress=True,
    ):
        """
        Helper that runs ModelDownloadMonitor._run synchronously and collects
        the values passed to both Prometheus gauge .set() calls.

        Returns (speed_calls, eta_calls).

        _model_cache_size_bytes is mocked to consume dir_sizes in order on
        every call (initial + each loop iteration).

        download_in_progress controls the value returned by the patched
        _download_in_progress helper.  Pass a bool for a constant value, or
        an iterable to return different values across loop iterations (used
        to simulate a download that completes mid-monitoring).  Set
        repo_path=None to simulate the case where the cache dir does not
        exist yet (falls back to expected_bytes-based ETA).
        """
        monitor = ModelDownloadMonitor(watch_dir="/fake/weights")
        speed_calls = []
        eta_calls = []

        speed_gauge = MagicMock(name="speed_gauge")
        speed_gauge.set.side_effect = speed_calls.append
        eta_gauge = MagicMock(name="eta_gauge")
        eta_gauge.set.side_effect = eta_calls.append

        size_iter = iter(dir_sizes)
        ts_iter = iter(timestamps)

        # _model_cache_size_bytes is called for every size sample (initial + loop).
        def _mock_cache_size(*_args, **_kwargs):
            return next(size_iter)

        if isinstance(download_in_progress, bool):
            dip_kwargs = {"return_value": download_in_progress}
        else:
            dip_iter = iter(download_in_progress)
            dip_kwargs = {"side_effect": lambda *_a, **_k: next(dip_iter)}

        with (
            patch(
                "inference_api._model_cache_size_bytes", side_effect=_mock_cache_size
            ),
            patch("inference_api._model_repo_path", return_value=repo_path),
            patch("inference_api._download_in_progress", **dip_kwargs),
            patch("inference_api._hf_model_total_bytes", return_value=total_bytes),
            patch("time.monotonic", side_effect=lambda: next(ts_iter)),
            patch.object(monitor._stop, "wait", side_effect=wait_returns),
            patch.object(
                inference_api,
                "kaito_model_download_speed_bytes_per_second",
                speed_gauge,
            ),
            patch.object(
                inference_api,
                "kaito_model_download_remaining_seconds",
                eta_gauge,
            ),
        ):
            monitor._run(model_id=model_id, hf_token=None)

        return speed_calls, eta_calls

    def test_speed_rolling_average_two_iterations(self):
        # Two identical intervals: 100 MiB downloaded in 5 s each → 20 MiB/s
        speed_calls, _ = self._run_monitor(
            dir_sizes=[0, 100 * MB, 200 * MB],
            timestamps=[0.0, 5.0, 10.0],
            wait_returns=[False, False, True],
            model_id="org/model",
        )
        expected = 100 * MB / 5.0
        assert len(speed_calls) == 2
        assert speed_calls[0] == pytest.approx(expected)
        assert speed_calls[1] == pytest.approx(expected)

    def test_eta_computed_when_total_size_known(self):
        # Total 500 MiB, first sample downloads 100 MiB in 5 s
        # speed = 20 MiB/s  →  ETA = 400 MiB / 20 MiB/s = 20 s
        TOTAL = 500 * MB
        DOWNLOADED = 100 * MB
        _, eta_calls = self._run_monitor(
            dir_sizes=[0, DOWNLOADED],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=TOTAL,
        )
        expected_eta = (TOTAL - DOWNLOADED) / (DOWNLOADED / 5.0)
        assert len(eta_calls) == 1
        assert eta_calls[0] == pytest.approx(expected_eta)

    def test_eta_is_minus_one_when_total_size_unknown(self):
        _, eta_calls = self._run_monitor(
            dir_sizes=[0, 100 * MB],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=None,
        )
        assert eta_calls == [-1]

    def test_eta_is_minus_one_when_speed_stays_zero(self):
        # Download stalled: bytes on disk didn't change, so avg_speed=0.
        # Even with a known total, we cannot compute a meaningful ETA →
        # publish -1 (sentinel for "unknown").
        DOWNLOADED = 100 * MB
        _, eta_calls = self._run_monitor(
            dir_sizes=[DOWNLOADED, DOWNLOADED],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=500 * MB,
        )
        assert eta_calls == [-1]

    def test_zero_metrics_when_no_model_id(self):
        # model_id=None means a local path — both metrics are set to 0 immediately.
        speed_calls, eta_calls = self._run_monitor(
            dir_sizes=[0],
            timestamps=[],
            wait_returns=[],
            model_id=None,
        )
        assert speed_calls == [0]
        assert eta_calls == [0]

    def test_zero_metrics_for_absolute_model_path(self):
        # Absolute path → local weights, no download.  Both metrics set
        # to 0 once and the loop never runs (wait_returns is empty so
        # any iteration would raise StopIteration).
        speed_calls, eta_calls = self._run_monitor(
            dir_sizes=[],
            timestamps=[],
            wait_returns=[],
            model_id="/workspace/vllm/weights",
        )
        assert speed_calls == [0]
        assert eta_calls == [0]

    def test_speed_is_zero_when_directory_unchanged(self):
        speed_calls, _ = self._run_monitor(
            dir_sizes=[500, 500],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
        )
        assert speed_calls == [0.0]

    def test_speed_never_negative(self):
        # Simulate a transient size decrease (e.g. partial file rewrite)
        speed_calls, _ = self._run_monitor(
            dir_sizes=[200 * MB, 100 * MB],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
        )
        assert all(v >= 0.0 for v in speed_calls)

    def test_eta_clamps_to_zero_when_download_exceeds_total(self):
        # downloaded > total (race between HF metadata and actual files)
        TOTAL = 50 * MB
        DOWNLOADED = 100 * MB  # more than total
        _, eta_calls = self._run_monitor(
            dir_sizes=[0, DOWNLOADED],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=TOTAL,
        )
        assert eta_calls[0] == pytest.approx(0.0)

    def test_eta_correct_with_nonzero_initial_size(self):
        # 50 MiB already present when monitoring starts (partial prior download).
        # expected_bytes is the FULL remote model size; remaining = expected - current_on_disk.
        TOTAL = 450 * MB
        INITIAL = 50 * MB
        DELTA = 100 * MB
        _, eta_calls = self._run_monitor(
            dir_sizes=[INITIAL, INITIAL + DELTA],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=TOTAL,
        )
        speed = DELTA / 5.0
        # remaining = TOTAL - current_size = 450MB - 150MB = 300MB
        expected_eta = (TOTAL - (INITIAL + DELTA)) / speed
        assert len(eta_calls) == 1
        assert eta_calls[0] == pytest.approx(expected_eta)

    def test_eta_zero_when_no_incomplete_blobs_even_if_total_overcounts(self):
        # Regression: HF repos that ship duplicate weight formats
        # (e.g. pytorch_model-*.bin AND model-*.safetensors) make
        # expected_bytes larger than what vLLM actually downloads.  The
        # authoritative completion signal — absence of *.incomplete blobs —
        # must drive ETA to 0 regardless of expected_bytes.
        INFLATED_TOTAL = 1000 * MB
        ON_DISK = 500 * MB  # download is done, but smaller than INFLATED_TOTAL
        _, eta_calls = self._run_monitor(
            dir_sizes=[ON_DISK, ON_DISK],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=INFLATED_TOTAL,
            download_in_progress=False,
        )
        assert eta_calls == [0]

    def test_eta_uses_expected_bytes_when_repo_path_missing(self):
        # repo_path=None → cache dir not yet created, fall back to
        # expected_bytes-based ETA so we still publish a number during
        # the brief window before HF creates the repo dir.
        TOTAL = 500 * MB
        DOWNLOADED = 100 * MB
        _, eta_calls = self._run_monitor(
            dir_sizes=[0, DOWNLOADED],
            timestamps=[0.0, 5.0],
            wait_returns=[False, True],
            model_id="org/model",
            total_bytes=TOTAL,
            repo_path=None,
        )
        expected_eta = (TOTAL - DOWNLOADED) / (DOWNLOADED / 5.0)
        assert len(eta_calls) == 1
        assert eta_calls[0] == pytest.approx(expected_eta)

    def test_partial_resume_completes_mid_loop(self):
        # Real partial-download-resume: 200 MiB already on disk with an
        # *.incomplete blob still present.  After one more sample interval
        # the download finishes (HF renames the blob, no more *.incomplete).
        # Verify:
        #   * during the in-progress iteration ETA uses expected_bytes,
        #   * on the completing iteration ETA is set to exactly 0,
        #   * the loop breaks (wait_returns[2] is never consumed — if it
        #     were, StopIteration would surface from the size/ts iterators).
        TOTAL = 500 * MB
        INITIAL = 200 * MB
        AFTER_RESUME = 350 * MB
        FINAL = 500 * MB
        _, eta_calls = self._run_monitor(
            # initial sample + iter1 sample + iter2 sample.  No more reads
            # after break, so iterators are sized exactly to enforce that.
            dir_sizes=[INITIAL, AFTER_RESUME, FINAL],
            timestamps=[0.0, 5.0, 10.0],
            # wait_returns has an extra entry we expect NOT to be consumed.
            wait_returns=[False, False, "unreachable"],
            model_id="org/model",
            total_bytes=TOTAL,
            # iter1: still downloading; iter2: HF finished, blob renamed.
            download_in_progress=[True, False],
        )
        # iter1 ETA from expected_bytes; iter2 must be exactly 0.
        speed_iter1 = (AFTER_RESUME - INITIAL) / 5.0
        expected_eta_iter1 = (TOTAL - AFTER_RESUME) / speed_iter1
        assert len(eta_calls) == 2
        assert eta_calls[0] == pytest.approx(expected_eta_iter1)
        assert eta_calls[1] == 0


# ── _model_repo_path ──────────────────────────────────────────────────────────
class TestModelRepoPath:
    def test_returns_path_when_repo_present(self):
        repo = MagicMock()
        repo.repo_id = "org/model"
        repo.repo_path = Path("/cache/models--org--model")
        scan_result = MagicMock()
        scan_result.repos = [repo]
        with patch.object(inference_api, "scan_cache_dir", return_value=scan_result):
            assert (
                _model_repo_path("org/model", "/cache") == "/cache/models--org--model"
            )

    def test_returns_none_when_repo_missing(self):
        other = MagicMock()
        other.repo_id = "other/model"
        scan_result = MagicMock()
        scan_result.repos = [other]
        with patch.object(inference_api, "scan_cache_dir", return_value=scan_result):
            assert _model_repo_path("org/model", "/cache") is None

    def test_returns_none_when_model_id_empty(self):
        assert _model_repo_path(None, "/cache") is None
        assert _model_repo_path("", "/cache") is None

    def test_returns_none_on_scan_exception(self):
        with patch.object(inference_api, "scan_cache_dir", side_effect=OSError("boom")):
            assert _model_repo_path("org/model", "/cache") is None


# ── _download_in_progress ─────────────────────────────────────────────────────
class TestDownloadInProgress:
    def test_returns_false_when_repo_path_none(self):
        assert _download_in_progress(None) is False

    def test_returns_false_when_blobs_dir_missing(self, tmp_path):
        # repo dir exists but no blobs/ subdir yet
        assert _download_in_progress(str(tmp_path)) is False

    def test_returns_false_when_no_incomplete_blobs(self, tmp_path):
        blobs = tmp_path / "blobs"
        blobs.mkdir()
        (blobs / "abcdef1234").write_bytes(b"finished blob")
        assert _download_in_progress(str(tmp_path)) is False

    def test_returns_true_with_incomplete_blob(self, tmp_path):
        blobs = tmp_path / "blobs"
        blobs.mkdir()
        (blobs / "abcdef1234").write_bytes(b"done")
        (blobs / "abcdef5678.incomplete").write_bytes(b"partial")
        assert _download_in_progress(str(tmp_path)) is True


# ── _model_cache_size_bytes ───────────────────────────────────────────────────
class TestModelCacheSizeBytes:
    def test_returns_zero_when_repo_path_none(self):
        assert _model_cache_size_bytes(None) == 0

    def test_counts_only_non_symlink_files(self, tmp_path):
        # Mimic HF cache layout: blobs/ holds real bytes, snapshots/ has
        # symlinks pointing into blobs/.  We must count blobs/ only.
        blobs = tmp_path / "blobs"
        blobs.mkdir()
        blob = blobs / "abcdef"
        blob.write_bytes(b"x" * 1000)
        incomplete = blobs / "abcdef.incomplete"
        incomplete.write_bytes(b"x" * 200)

        snapshots = tmp_path / "snapshots" / "rev1"
        snapshots.mkdir(parents=True)
        try:
            (snapshots / "weights.bin").symlink_to(blob)
        except OSError:
            pytest.skip("symlinks not supported on this filesystem")

        # 1000 (blob) + 200 (incomplete); symlink ignored
        assert _model_cache_size_bytes(str(tmp_path)) == 1200


# ── get_max_gpu_memory_utilization / _query_gpu_mem_info ──────────────────────
class TestGpuMemoryUtilization:
    GiB = 1024**3

    def test_computes_utilization_from_free_memory(self):
        # 24 GiB total, 20 GiB free → (20GiB - 600MiB) / 24GiB, floored to 2dp.
        free = 20 * self.GiB
        total = 24 * self.GiB
        with patch.object(
            inference_api, "_query_gpu_mem_info", return_value=(free, total)
        ):
            util = inference_api.get_max_gpu_memory_utilization(0)
        expected = (((free - 600 * MB) * 100) // total) / 100
        assert util == expected
        assert util <= 0.95

    def test_caps_at_0_95(self):
        # A fully free GPU would exceed 0.95 → capped.
        free = total = 80 * self.GiB
        with patch.object(
            inference_api, "_query_gpu_mem_info", return_value=(free, total)
        ):
            assert inference_api.get_max_gpu_memory_utilization(0) == 0.95

    def test_falls_back_to_default_on_error(self):
        # A query failure must not crash startup (or initialize CUDA in this
        # process); fall back to the default utilization.
        with patch.object(
            inference_api, "_query_gpu_mem_info", side_effect=RuntimeError("boom")
        ):
            assert (
                inference_api.get_max_gpu_memory_utilization(0)
                == inference_api._DEFAULT_GPU_MEMORY_UTILIZATION
            )

    def test_query_parses_sentinel_line_from_subprocess(self):
        # _query_gpu_mem_info runs torch in a subprocess; verify it parses the
        # sentinel line (ignoring stray stdout) without spawning a real one.
        completed = MagicMock()
        completed.stdout = "some torch warning\nKAITO_MEM_INFO 123 456\n"
        with patch.object(
            inference_api.subprocess, "run", return_value=completed
        ) as mock_run:
            assert inference_api._query_gpu_mem_info(3) == (123, 456)
        # The device index is forwarded to the child process.
        assert mock_run.call_args[0][0][-1] == "3"

    def test_query_raises_on_unexpected_output(self):
        completed = MagicMock()
        completed.stdout = "no sentinel here\n"
        with (
            patch.object(inference_api.subprocess, "run", return_value=completed),
            pytest.raises(RuntimeError),
        ):
            inference_api._query_gpu_mem_info(0)
