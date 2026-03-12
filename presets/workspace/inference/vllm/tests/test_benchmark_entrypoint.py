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

import sys
from pathlib import Path
from types import SimpleNamespace
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


# ── _compute_rate ─────────────────────────────────────────────────────────────


def test_compute_rate_with_env(monkeypatch):
    monkeypatch.setenv("BENCHMARK_RATE", "256")
    assert bm._compute_rate() == 256


def test_compute_rate_fallback_no_env(monkeypatch):
    monkeypatch.delenv("BENCHMARK_RATE", raising=False)
    assert bm._compute_rate() == 512


def test_compute_rate_fallback_invalid_env(monkeypatch):
    monkeypatch.setenv("BENCHMARK_RATE", "notanumber")
    assert bm._compute_rate() == 512


def test_compute_rate_minimum_one(monkeypatch):
    monkeypatch.setenv("BENCHMARK_RATE", "0")
    assert bm._compute_rate() >= 1


# ── _run_guidellm ─────────────────────────────────────────────────────────────


def test_run_guidellm_success():
    proc = SimpleNamespace(returncode=0)
    with (
        patch("subprocess.run", return_value=proc) as mock_run,
        patch("os.path.isfile", return_value=False),
        patch("os.access", return_value=False),
    ):
        result = bm._run_guidellm("openai/phi-4", 256)
    assert result is True
    mock_run.assert_called_once()
    cmd = mock_run.call_args[0][0]
    assert cmd[0] == "guidellm"  # fell back to PATH


def test_run_guidellm_uses_venv_binary():
    proc = SimpleNamespace(returncode=0)
    with (
        patch("subprocess.run", return_value=proc) as mock_run,
        patch("os.path.isfile", return_value=True),
        patch("os.access", return_value=True),
    ):
        bm._run_guidellm("", 256)
    cmd = mock_run.call_args[0][0]
    assert cmd[0] == "/opt/guidellm-venv/bin/guidellm"


def test_run_guidellm_includes_processor():
    proc = SimpleNamespace(returncode=0)
    with (
        patch("subprocess.run", return_value=proc) as mock_run,
        patch("os.path.isfile", return_value=False),
        patch("os.access", return_value=False),
    ):
        bm._run_guidellm("mymodel/name", 128)
    cmd = mock_run.call_args[0][0]
    assert "--processor" in cmd
    assert "mymodel/name" in cmd


def test_run_guidellm_omits_processor_when_empty():
    proc = SimpleNamespace(returncode=0)
    with (
        patch("subprocess.run", return_value=proc) as mock_run,
        patch("os.path.isfile", return_value=False),
        patch("os.access", return_value=False),
    ):
        bm._run_guidellm("", 128)
    cmd = mock_run.call_args[0][0]
    assert "--processor" not in cmd


def test_run_guidellm_failure():
    proc = SimpleNamespace(returncode=1)
    with (
        patch("subprocess.run", return_value=proc),
        patch("os.path.isfile", return_value=False),
        patch("os.access", return_value=False),
        patch.object(bm, "_log") as mock_log,
    ):
        result = bm._run_guidellm("", 128)
    assert result is False
    mock_log.assert_called_once()
    assert "guidellm_failed" in mock_log.call_args[0][0]


def test_run_guidellm_sets_stub_env_var():
    """GUIDELLM__REPORT_GENERATION__SOURCE must point to the stub file."""
    captured_env = {}

    def fake_run(cmd, env, **kwargs):
        captured_env.update(env)
        return SimpleNamespace(returncode=0)

    with (
        patch("subprocess.run", side_effect=fake_run),
        patch("os.path.isfile", return_value=False),
        patch("os.access", return_value=False),
    ):
        bm._run_guidellm("", 32)

    assert "GUIDELLM__REPORT_GENERATION__SOURCE" in captured_env
    stub = captured_env["GUIDELLM__REPORT_GENERATION__SOURCE"]
    # The stub file must have been cleaned up
    assert not Path(stub).exists()


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

    with (
        patch.object(bm, "_read_counter", side_effect=read_counter),
        patch.object(bm, "_resolve_processor", return_value="mymodel"),
        patch.object(bm, "_compute_rate", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=True),
        patch.object(bm, "_log"),
        patch(
            "time.time",
            side_effect=[0.0, 60.0],  # t0, t1 → 60 s elapsed
        ),
    ):
        tpm = bm._run_benchmark()

    # (6000 + 24576) * 60 / 60 = 30576.0
    assert tpm == pytest.approx(30576.0)


def test_run_benchmark_no_generation():
    with (
        patch.object(bm, "_read_counter", return_value=0),
        patch.object(bm, "_resolve_processor", return_value=""),
        patch.object(bm, "_compute_rate", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=True),
        patch.object(bm, "_log"),
        patch("time.time", side_effect=[0.0, 60.0]),
        pytest.raises(RuntimeError, match="no_generation"),
    ):
        bm._run_benchmark()


def test_run_benchmark_guidellm_fails():
    with (
        patch.object(bm, "_read_counter", return_value=0),
        patch.object(bm, "_resolve_processor", return_value=""),
        patch.object(bm, "_compute_rate", return_value=128),
        patch.object(bm, "_run_guidellm", return_value=False),
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
        patch.object(bm, "_run_benchmark", return_value=12345.67),
        patch.object(bm, "_drain"),
        patch.object(bm, "_log"),
        patch.object(bm, "_write_to_pid1", side_effect=fake_write),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()

    assert exc_info.value.code == 0
    result_lines = [line for line in written if "KAITO_BENCHMARK_RESULT" in line]
    assert len(result_lines) == 1
    assert "vllm_total_tpm=12345.67" in result_lines[0]


def test_main_benchmark_failure_still_exits_0(monkeypatch):
    """If the benchmark raises, result is -1 but exit code is still 0."""
    monkeypatch.delenv("POD_INDEX", raising=False)
    written = []

    def fake_write(line, fd=1):
        written.append(line)

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", side_effect=RuntimeError("guidellm failed")),
        patch.object(bm, "_log"),
        patch.object(bm, "_write_to_pid1", side_effect=fake_write),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit) as exc_info,
    ):
        bm.main()

    assert exc_info.value.code == 0
    result_lines = [line for line in written if "KAITO_BENCHMARK_RESULT" in line]
    assert len(result_lines) == 1
    assert "vllm_total_tpm=-1" in result_lines[0]


def test_main_exactly_one_result_line_on_success(monkeypatch):
    """Exactly one KAITO_BENCHMARK_RESULT line is printed per invocation."""
    monkeypatch.delenv("POD_INDEX", raising=False)
    written = []

    with (
        patch.object(bm, "_health_check", return_value=True),
        patch.object(bm, "_run_benchmark", return_value=999.0),
        patch.object(bm, "_drain"),
        patch.object(bm, "_log"),
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
        patch.object(bm, "_log"),
        patch.object(bm, "_write_to_pid1"),
        patch("time.time", return_value=0.0),
        pytest.raises(SystemExit),
    ):
        bm.main()

    mock_drain.assert_not_called()
