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

"""Unit tests for benchmark_entrypoint.py.

All tests run without a GPU, network, vLLM process, or guidellm installation.
External calls (urllib, subprocess, open) are patched via unittest.mock.
"""

import json
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

# Add the parent directory (presets/workspace/inference/vllm) to sys.path so we
# can import benchmark_entrypoint directly.
_SCRIPT_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_SCRIPT_DIR))

import benchmark_entrypoint as bm  # noqa: E402, I001


# ── Helpers ───────────────────────────────────────────────────────────────────


def _make_urlopen_response(status: int, body: bytes = b""):
    """Return a minimal context-manager mock for urllib.request.urlopen."""
    resp = MagicMock()
    resp.status = status
    resp.read.return_value = body
    resp.__enter__ = lambda self: self
    resp.__exit__ = MagicMock(return_value=False)
    return resp


# ── _health_check ─────────────────────────────────────────────────────────────


def test_health_check_success():
    resp = _make_urlopen_response(200)
    with patch("urllib.request.urlopen", return_value=resp):
        assert bm._health_check() is True


def test_health_check_non200():
    resp = _make_urlopen_response(503)
    with patch("urllib.request.urlopen", return_value=resp):
        assert bm._health_check() is False


def test_health_check_exception():
    with patch("urllib.request.urlopen", side_effect=OSError("refused")):
        assert bm._health_check() is False


# ── _log ──────────────────────────────────────────────────────────────────────


def test_log_exits_1_on_write_failure():
    """If _write_to_pid1 raises, _log calls sys.exit(1) instead of propagating."""
    with (
        patch.object(bm, "_write_to_pid1", side_effect=OSError("no /proc/1/fd/1")),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm._log("some message")
    assert exc_info.value.code == 1


# ── _read_counter ─────────────────────────────────────────────────────────────

_METRICS_BODY = b"""\
# HELP vllm:generation_tokens_total Number of generation tokens processed.
# TYPE vllm:generation_tokens_total counter
vllm:generation_tokens_total{engine="0",model_name="phi-4"} 987654.0
# HELP vllm:prompt_tokens_total Number of prefill tokens processed.
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total{engine="0",model_name="phi-4"} 12345.0
# HELP vllm:num_requests_running Number of requests in model execution batches.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{engine="0",model_name="phi-4"} 3.0
process_open_fds 47.0
"""


def test_read_counter_labelled():
    resp = _make_urlopen_response(200, _METRICS_BODY)
    with patch("urllib.request.urlopen", return_value=resp):
        assert bm._read_counter("vllm:generation_tokens_total") == 987654


def test_read_counter_bare():
    """process_open_fds is a real bare (no-label) metric in vLLM's /metrics output."""
    resp = _make_urlopen_response(200, _METRICS_BODY)
    with patch("urllib.request.urlopen", return_value=resp):
        assert bm._read_counter("process_open_fds") == 47


def test_read_counter_not_found():
    resp = _make_urlopen_response(200, _METRICS_BODY)
    with patch("urllib.request.urlopen", return_value=resp):
        assert bm._read_counter("vllm:nonexistent_metric") == 0


def test_read_counter_network_error():
    with patch("urllib.request.urlopen", side_effect=OSError("timeout")):
        assert bm._read_counter("vllm:generation_tokens_total") == 0


# ── _compute_max_concurrency ──────────────────────────────────────────────────


def test_compute_max_concurrency_with_env(monkeypatch):
    monkeypatch.setenv("BENCHMARK_MAX_CONCURRENCY", "256")
    assert bm._compute_max_concurrency() == 256


def test_compute_max_concurrency_missing_env_raises(monkeypatch):
    monkeypatch.delenv("BENCHMARK_MAX_CONCURRENCY", raising=False)
    with pytest.raises(RuntimeError, match="invalid BENCHMARK_MAX_CONCURRENCY"):
        bm._compute_max_concurrency()


def test_compute_max_concurrency_invalid_env_raises(monkeypatch):
    monkeypatch.setenv("BENCHMARK_MAX_CONCURRENCY", "notanumber")
    with pytest.raises(RuntimeError, match="invalid BENCHMARK_MAX_CONCURRENCY"):
        bm._compute_max_concurrency()


def test_compute_max_concurrency_zero_raises(monkeypatch):
    monkeypatch.setenv("BENCHMARK_MAX_CONCURRENCY", "0")
    with pytest.raises(RuntimeError, match="invalid BENCHMARK_MAX_CONCURRENCY"):
        bm._compute_max_concurrency()


# ── _run_guidellm ─────────────────────────────────────────────────────────────


def _guidellm_sys_modules(mock_args_cls, mock_benchmark_fn):
    """Build a sys.modules patch dict that injects mock guidellm packages."""
    bench_mod = MagicMock()
    bench_mod.BenchmarkGenerativeTextArgs = mock_args_cls
    entrypoints_mod = MagicMock()
    entrypoints_mod.benchmark_generative_text = mock_benchmark_fn
    return {
        "guidellm": MagicMock(),
        "guidellm.benchmark": bench_mod,
        "guidellm.benchmark.entrypoints": entrypoints_mod,
    }


def test_run_guidellm_success():
    mock_args_cls = MagicMock()
    mock_benchmark_fn = MagicMock()
    mock_report = MagicMock(name="report")
    with (
        patch("asyncio.run", return_value=(mock_report, {})) as mock_run,
        patch.dict(
            sys.modules, _guidellm_sys_modules(mock_args_cls, mock_benchmark_fn)
        ),
    ):
        result = bm._run_guidellm("openai/phi-4", 256)
    assert result is mock_report
    mock_run.assert_called_once()


def test_run_guidellm_includes_processor():
    mock_args_cls = MagicMock()
    mock_benchmark_fn = MagicMock()
    with (
        patch("asyncio.run", return_value=(MagicMock(), {})),
        patch.dict(
            sys.modules, _guidellm_sys_modules(mock_args_cls, mock_benchmark_fn)
        ),
    ):
        bm._run_guidellm("mymodel/name", 128)
    _, kwargs = mock_args_cls.call_args
    assert kwargs.get("processor") == "mymodel/name"


def test_run_guidellm_omits_processor_when_empty():
    mock_args_cls = MagicMock()
    mock_benchmark_fn = MagicMock()
    with (
        patch("asyncio.run", return_value=(MagicMock(), {})),
        patch.dict(
            sys.modules, _guidellm_sys_modules(mock_args_cls, mock_benchmark_fn)
        ),
    ):
        bm._run_guidellm("", 128)
    _, kwargs = mock_args_cls.call_args
    assert kwargs.get("processor") is None


def test_run_guidellm_failure():
    mock_args_cls = MagicMock()
    mock_benchmark_fn = MagicMock()
    with (
        patch("asyncio.run", side_effect=RuntimeError("mock error")),
        patch.dict(
            sys.modules, _guidellm_sys_modules(mock_args_cls, mock_benchmark_fn)
        ),
        patch.object(bm, "_log") as mock_log,
    ):
        result = bm._run_guidellm("", 128)
    assert result is None
    mock_log.assert_called_once()
    assert "guidellm_failed" in mock_log.call_args[0][0]


def test_run_guidellm_import_error():
    """If guidellm is not importable, _run_guidellm returns False and logs."""
    with (
        patch.dict(
            sys.modules,
            {
                "guidellm": None,
                "guidellm.benchmark": None,
                "guidellm.benchmark.entrypoints": None,
            },
        ),
        patch.object(bm, "_log") as mock_log,
    ):
        result = bm._run_guidellm("", 32)
    assert result is None
    mock_log.assert_called_once()
    assert "guidellm_import_failed" in mock_log.call_args[0][0]


# ── _extract_guidellm_metrics ────────────────────────────────────────────────


def _mock_report(ttft_mean=42.123, tpot_mean=3.456):
    """Build a mock guidellm report with the given TTFT/TPOT mean values."""
    report = MagicMock()
    report.benchmarks = [MagicMock()]
    metrics = report.benchmarks[0].metrics
    metrics.time_to_first_token_ms.total.mean = ttft_mean
    metrics.time_per_output_token_ms.total.mean = tpot_mean
    return report


def test_extract_guidellm_metrics_success():
    report = _mock_report(ttft_mean=42.123, tpot_mean=3.456)
    ttft, tpot = bm._extract_guidellm_metrics(report)
    assert ttft == 42.12
    assert tpot == 3.46


def test_extract_guidellm_metrics_empty_benchmarks():
    report = MagicMock()
    report.benchmarks = []
    with pytest.raises(RuntimeError, match="failed to extract TTFT/TPOT"):
        bm._extract_guidellm_metrics(report)


def test_extract_guidellm_metrics_none_total():
    report = MagicMock()
    report.benchmarks = [MagicMock()]
    report.benchmarks[0].metrics.time_to_first_token_ms.total = None
    with pytest.raises(RuntimeError, match="failed to extract TTFT/TPOT"):
        bm._extract_guidellm_metrics(report)


# ── _run_benchmark ───────────────────────────────────────────────────────────


def test_run_benchmark_success(monkeypatch):
    call_count = [0]

    def read_counter(metric):
        call_count[0] += 1
        # First two calls return t0 values, next two return t1 values
        if call_count[0] <= 2:
            return 0
        if metric == "vllm:generation_tokens_total":
            return 6000
        if metric == "vllm:prompt_tokens_total":
            return 24576
        return 0

    mock_report = _mock_report(ttft_mean=42.123, tpot_mean=3.456)
    with (
        patch.object(bm, "_read_counter", side_effect=read_counter),
        patch.object(bm, "_resolve_processor", return_value="mymodel"),
        patch.object(bm, "_compute_max_concurrency", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=mock_report),
        patch.object(bm, "_log"),
        patch(
            "time.time",
            side_effect=[0.0, 60.0],  # t0, t1 → 60 s elapsed
        ),
    ):
        tpm, ttft, tpot, max_concurrency = bm._run_benchmark()

    # (6000 + 24576) * 60 / 60 = 30576.0
    assert tpm == pytest.approx(30576.0)
    assert ttft == 42.12
    assert tpot == 3.46
    assert max_concurrency == 128


def test_run_benchmark_no_generation():
    mock_report = _mock_report()
    with (
        patch.object(bm, "_read_counter", return_value=0),
        patch.object(bm, "_resolve_processor", return_value=""),
        patch.object(bm, "_compute_max_concurrency", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=mock_report),
        patch.object(bm, "_log"),
        patch("time.time", side_effect=[0.0, 60.0]),
        pytest.raises(RuntimeError, match="no_generation"),
    ):
        bm._run_benchmark()


def test_run_benchmark_guidellm_fails():
    with (
        patch.object(bm, "_read_counter", return_value=0),
        patch.object(bm, "_resolve_processor", return_value=""),
        patch.object(bm, "_compute_max_concurrency", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=None),
        patch.object(bm, "_log"),
        patch("time.time", return_value=0.0),
        pytest.raises(RuntimeError, match="guidellm"),
    ):
        bm._run_benchmark()


# ── _drain ───────────────────────────────────────────────────────────────────


def test_drain_already_zero():
    with (
        patch.object(bm, "_read_counter", return_value=0),
        patch.object(bm, "_log"),
        patch("time.sleep") as mock_sleep,
    ):
        bm._drain()
    mock_sleep.assert_not_called()


def test_drain_polls_until_zero():
    # Returns 3, 3, 0 on successive calls
    counter_calls = [3, 3, 0]
    with (
        patch.object(bm, "_read_counter", side_effect=counter_calls),
        patch.object(bm, "_log"),
        patch("time.sleep") as mock_sleep,
    ):
        bm._drain()
    assert mock_sleep.call_count == 2
    mock_sleep.assert_called_with(2)


def test_drain_timeout():
    """_drain raises TimeoutError once the deadline is passed."""
    # monotonic() returns: initial call (deadline set), then past-deadline on second call
    mono_values = [0.0, 301.0]
    with (
        patch.object(bm, "_read_counter", return_value=1),
        patch.object(bm, "_log"),
        patch("time.sleep"),
        patch("time.monotonic", side_effect=mono_values),
        pytest.raises(TimeoutError, match="_drain timed out after 300"),
    ):
        bm._drain(timeout=300.0)


# ── main ─────────────────────────────────────────────────────────────────────


def test_main_worker_skips_benchmark(monkeypatch):
    """Workers (POD_INDEX != 0) exit 0 immediately without any HTTP."""
    monkeypatch.setenv("POD_INDEX", "1")
    with (
        patch("urllib.request.urlopen") as mock_url,
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()
    assert exc_info.value.code == 0
    mock_url.assert_not_called()


def test_main_health_fails_exits_1(monkeypatch):
    monkeypatch.delenv("POD_INDEX", raising=False)
    with (
        patch.object(bm, "_health_check", return_value=False),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()
    assert exc_info.value.code == 1


def test_main_benchmark_success_exits_0(monkeypatch):
    monkeypatch.delenv("POD_INDEX", raising=False)
    written = []

    def fake_write(line, fd=1):
        written.append(line)

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", return_value=(12345.67, 42.12, 3.46, 256)),
        patch.object(bm, "_drain"),
        patch.object(bm, "_write_to_pid1", side_effect=fake_write),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()

    assert exc_info.value.code == 0
    result_lines = [line for line in written if "KAITO_BENCHMARK_RESULT" in line]
    assert len(result_lines) == 1
    payload = result_lines[0].split("KAITO_BENCHMARK_RESULT ", 1)[1]
    data = json.loads(payload[payload.index("{") :])
    assert data["vllm_total_tpm"] == 12345.67
    assert data["ttft_avg_ms"] == 42.12
    assert data["tpot_avg_ms"] == 3.46


def test_main_benchmark_failure_exits_1(monkeypatch):
    """If the benchmark raises, result line is -1 and the probe exits 1 (fail-close)."""
    monkeypatch.delenv("POD_INDEX", raising=False)
    written = []

    def fake_write(line, fd=1):
        written.append(line)

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", side_effect=RuntimeError("guidellm failed")),
        patch.object(bm, "_write_to_pid1", side_effect=fake_write),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()

    assert exc_info.value.code == 1
    result_lines = [line for line in written if "KAITO_BENCHMARK_RESULT" in line]
    assert len(result_lines) == 1
    payload = result_lines[0].split("KAITO_BENCHMARK_RESULT ", 1)[1]
    data = json.loads(payload[payload.index("{") :])
    assert data["vllm_total_tpm"] == -1.0
    assert data["ttft_avg_ms"] == -1.0
    assert data["tpot_avg_ms"] == -1.0


def test_main_exactly_one_result_line_on_success(monkeypatch):
    """Exactly one KAITO_BENCHMARK_RESULT line is printed per invocation."""
    monkeypatch.delenv("POD_INDEX", raising=False)
    written = []

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", return_value=(999.0, 10.0, 2.0, 128)),
        patch.object(bm, "_drain"),
        patch.object(
            bm, "_write_to_pid1", side_effect=lambda line, fd=1: written.append(line)
        ),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit),
    ):
        bm.main()

    result_lines = [line for line in written if "KAITO_BENCHMARK_RESULT" in line]
    assert len(result_lines) == 1


def test_main_drain_called_only_on_success(monkeypatch):
    """_drain should only be called when _run_benchmark succeeds."""
    monkeypatch.delenv("POD_INDEX", raising=False)

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", side_effect=RuntimeError("fail")),
        patch.object(bm, "_drain") as mock_drain,
        patch.object(bm, "_write_to_pid1"),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit),
    ):
        bm.main()

    mock_drain.assert_not_called()
