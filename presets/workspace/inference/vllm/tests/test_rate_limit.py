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

"""Unit tests for the queue-depth rate limit middleware.

starlette and vllm are stubbed via sys.modules so these run on plain dev
machines. prometheus_client is used for real — the module registers a
Counter on it at import time.
"""

import asyncio
import sys
import types
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest
from prometheus_client import CollectorRegistry, Gauge


# ── Stubs ────────────────────────────────────────────────────────────────────
# BaseHTTPMiddleware must be a real class (rate_limit subclasses it).
class _StubBaseHTTPMiddleware:
    def __init__(self, app=None):
        self.app = app


class _StubJSONResponse:
    def __init__(self, status_code, content):
        self.status_code = status_code
        self.content = content


_starlette_base = types.ModuleType("starlette.middleware.base")
_starlette_base.BaseHTTPMiddleware = _StubBaseHTTPMiddleware
_starlette_responses = types.ModuleType("starlette.responses")
_starlette_responses.JSONResponse = _StubJSONResponse
_starlette_middleware = types.ModuleType("starlette.middleware")
_starlette = types.ModuleType("starlette")

# Shared registry so both the module's Counter and the test-injected Gauge
# live in the same place, and _current_num_requests_waiting can find it.
_TEST_REGISTRY = CollectorRegistry()
_vllm_prom = types.ModuleType("vllm.v1.metrics.prometheus")
_vllm_prom.get_prometheus_registry = lambda: _TEST_REGISTRY
_vllm_metrics = types.ModuleType("vllm.v1.metrics")
_vllm_v1 = types.ModuleType("vllm.v1")
_vllm = types.ModuleType("vllm")

for _name, _mod in {
    "starlette": _starlette,
    "starlette.middleware": _starlette_middleware,
    "starlette.middleware.base": _starlette_base,
    "starlette.responses": _starlette_responses,
    "vllm": _vllm,
    "vllm.v1": _vllm_v1,
    "vllm.v1.metrics": _vllm_metrics,
    "vllm.v1.metrics.prometheus": _vllm_prom,
}.items():
    sys.modules.setdefault(_name, _mod)

# A sibling test module may have registered its own starlette stub first via
# setdefault. Use whatever JSONResponse class rate_limit actually imported —
# not the local one, which may not match.
_StubJSONResponse = sys.modules["starlette.responses"].JSONResponse

_PARENT = str(Path(__file__).resolve().parent.parent)
if _PARENT not in sys.path:
    sys.path.insert(0, _PARENT)

import rate_limit  # noqa: E402

# If a sibling test module imported rate_limit first with a MagicMock stub of
# vllm.v1.metrics.prometheus, rate_limit._registry ended up as a MagicMock and
# won't hold our gauge. Force it to _TEST_REGISTRY so tests are order-independent.
rate_limit._registry = _TEST_REGISTRY

# The metric the middleware reads. Registered in the same test registry.
_WAITING_GAUGE = Gauge(
    "vllm:num_requests_waiting",
    "test double for vllm waiting queue gauge",
    labelnames=["model_name"],
    registry=_TEST_REGISTRY,
)


# ── Helpers ──────────────────────────────────────────────────────────────────
def _make_request(path="/v1/chat/completions"):
    return SimpleNamespace(url=SimpleNamespace(path=path))


def _run(coro):
    return asyncio.new_event_loop().run_until_complete(coro)


@pytest.fixture(autouse=True)
def _reset_state():
    """Ensure test isolation on module-level mutable state."""
    rate_limit._max_num_seqs = None
    rate_limit._pending = 0
    _WAITING_GAUGE.clear()
    yield
    rate_limit._max_num_seqs = None
    rate_limit._pending = 0
    _WAITING_GAUGE.clear()


def _set_waiting(value, model="test-model"):
    _WAITING_GAUGE.labels(model_name=model).set(value)


# ── configure ────────────────────────────────────────────────────────────────
class TestConfigure:
    def test_sets_threshold(self):
        rate_limit.configure(42)
        assert rate_limit._max_num_seqs == 42

    def test_coerces_to_int(self):
        rate_limit.configure("128")
        assert rate_limit._max_num_seqs == 128


# ── _current_num_requests_waiting ────────────────────────────────────────────
class TestCurrentNumRequestsWaiting:
    def test_reads_gauge_value(self):
        _set_waiting(7)
        assert rate_limit._current_num_requests_waiting() == 7

    def test_sums_across_label_sets(self):
        _set_waiting(3, model="a")
        _set_waiting(5, model="b")
        assert rate_limit._current_num_requests_waiting() == 8

    def test_returns_zero_when_metric_missing(self, monkeypatch):
        empty = CollectorRegistry()
        monkeypatch.setattr(rate_limit, "_registry", empty)
        assert rate_limit._current_num_requests_waiting() == 0.0


# ── Middleware dispatch ──────────────────────────────────────────────────────
class TestDispatchNoOp:
    def test_noop_when_threshold_not_configured(self):
        # configure() never called → _max_num_seqs is None → pass through.
        called = []

        async def call_next(_req):
            called.append(True)
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        result = _run(mw.dispatch(_make_request(), call_next))
        assert result == "OK"
        assert called == [True]

    def test_passes_through_non_guarded_path(self):
        rate_limit.configure(4)
        _set_waiting(1000)  # Would trip if guarded.

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        for path in ("/health", "/metrics", "/v1/models"):
            result = _run(mw.dispatch(_make_request(path), call_next))
            assert result == "OK", f"path={path} should be unguarded"


class TestDispatchAdmit:
    def test_admits_when_metric_at_threshold(self):
        # condition is strictly > threshold, so equality admits.
        rate_limit.configure(4)
        _set_waiting(4)

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        result = _run(mw.dispatch(_make_request(), call_next))
        assert result == "OK"

    def test_admits_when_metric_below_threshold(self):
        rate_limit.configure(4)
        _set_waiting(1)

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        result = _run(mw.dispatch(_make_request(), call_next))
        assert result == "OK"

    def test_pending_decrements_on_success(self):
        rate_limit.configure(4)
        _set_waiting(0)

        async def call_next(_req):
            assert rate_limit._pending == 1
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        _run(mw.dispatch(_make_request(), call_next))
        assert rate_limit._pending == 0

    def test_pending_decrements_on_downstream_exception(self):
        rate_limit.configure(4)
        _set_waiting(0)

        async def call_next(_req):
            raise RuntimeError("boom")

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        with pytest.raises(RuntimeError):
            _run(mw.dispatch(_make_request(), call_next))
        assert rate_limit._pending == 0


class TestDispatchReject:
    def test_rejects_when_metric_exceeds_threshold(self):
        rate_limit.configure(4)
        _set_waiting(5)

        called = []

        async def call_next(_req):
            called.append(True)
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        resp = _run(mw.dispatch(_make_request(), call_next))
        assert isinstance(resp, _StubJSONResponse)
        assert resp.status_code == 429
        assert resp.content["error"]["code"] == "rate_limit_exceeded"
        assert called == []
        assert rate_limit._pending == 0  # rejected admits never increment

    def test_rejected_counter_increments(self):
        rate_limit.configure(4)
        _set_waiting(10)
        before = rate_limit.kaito_ratelimit_rejected_total._value.get()

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        _run(mw.dispatch(_make_request(), call_next))

        after = rate_limit.kaito_ratelimit_rejected_total._value.get()
        assert after == before + 1


class TestDispatchLocalHedge:
    """The local _pending hedge: metric lags, so during a burst
    ``_pending - threshold`` must dominate."""

    def test_local_estimate_rejects_when_metric_still_stale(self):
        # Simulate a burst: metric has not caught up (0), but _pending has
        # already ballooned past 2 * threshold. Effective waiting must
        # reflect the local overflow.
        rate_limit.configure(4)
        _set_waiting(0)
        rate_limit._pending = 9  # local_waiting = 5 > threshold 4 → reject

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        resp = _run(mw.dispatch(_make_request(), call_next))
        assert isinstance(resp, _StubJSONResponse)
        assert resp.status_code == 429

    def test_metric_wins_when_higher_than_local(self):
        # Scheduler has caught up: metric high, no local pressure.
        rate_limit.configure(4)
        _set_waiting(100)
        rate_limit._pending = 0

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        resp = _run(mw.dispatch(_make_request(), call_next))
        assert isinstance(resp, _StubJSONResponse)
        assert resp.status_code == 429

    def test_local_below_threshold_admits(self):
        # _pending high but not yet overflowing batch capacity → local=0.
        rate_limit.configure(4)
        _set_waiting(0)
        rate_limit._pending = 4  # local_waiting = max(0, 4-4) = 0

        async def call_next(_req):
            return "OK"

        mw = rate_limit.RateLimitMiddleware(app=MagicMock())
        result = _run(mw.dispatch(_make_request(), call_next))
        assert result == "OK"
