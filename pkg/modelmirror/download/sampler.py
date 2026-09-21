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

"""Samples model-download progress and serves it as Prometheus gauges.

Runs as a native sidecar alongside the `hf download` container, reading the
shared model PVC.
"""

import fnmatch
import os
import signal
import sys
import time

from prometheus_client import (
    CollectorRegistry,
    Gauge,
    generate_latest,
    start_http_server,
)

# huggingface_hub creates a .incomplete file per *file*, not per download: the
# path is derived from that file's etag and opened when its transfer starts, then
# atomically renamed to the destination when it ends (file_download.py:1852 in
# v1.18.0, the version pinned in pkg/modelmirror/consts/consts.go). There is no
# task-level marker, so their presence means "some file is mid-transfer" -- see
# ProgressState.set_finished for why that is not the same as "download running".
INCOMPLETE_SUBDIR = os.path.join(".cache", "huggingface", "download")


def directory_bytes(path):
    """Total size of regular files under path. Symlinks are skipped so a linked
    blob is not counted twice."""
    total = 0
    for root, _dirs, files in os.walk(path):
        for name in files:
            fp = os.path.join(root, name)
            if os.path.islink(fp):
                continue
            try:
                total += os.path.getsize(fp)
            except OSError:
                # The downloader may rename a file out from under the walk.
                continue
    return total


def has_incomplete(path):
    """True when any *.incomplete file exists under the model directory."""
    incomplete_dir = os.path.join(path, INCOMPLETE_SUBDIR)
    for root, _dirs, files in os.walk(incomplete_dir):
        for name in files:
            if name.endswith(".incomplete"):
                return True
    return False


def is_finished(path):
    """True when the download has completed.

    Both conjuncts are required. At t=0 the downloader has not created any
    .incomplete files yet, so testing only for their absence would report
    "finished" before the download starts.
    """
    return directory_bytes(path) > 0 and not has_incomplete(path)


class ProgressState:
    """Accumulates samples and derives speed and ETA.

    Speed is a cumulative average over the whole run. Over a multi-minute download
    the cumulative figure is stabler and less sensitive to a momentary stall.
    """

    def __init__(self, baseline_bytes, start_time, total_bytes=None):
        # baseline_bytes is the directory size at sidecar startup. Growth is
        # measured relative to it so a Job retry reports only the bytes that
        # attempt actually transferred.
        self._baseline = baseline_bytes
        self._start = start_time
        self._total = total_bytes
        self._current = baseline_bytes
        self._now = start_time
        self._finished = False

    def update(self, current_bytes, now):
        self._current = current_bytes
        self._now = now

    def set_total(self, total_bytes):
        self._total = total_bytes

    def set_finished(self, finished):
        """Set the finished flag from the current sample.

        Deliberately not a latch, because zero *.incomplete files does not imply
        the download is over. snapshot_download runs a plain thread pool over the
        file list, and each worker holds no .incomplete file between renaming one file
        and opening the next.

        Latching on that transient would freeze 0/0 in status for the rest of the
        run. Re-evaluating each sample costs at most one wrong 0/0 reading, which
        self-corrects on the next sample.
        """
        self._finished = finished

    def speed_bytes_per_second(self):
        if self._finished:
            return 0.0
        elapsed = self._now - self._start
        if elapsed <= 0:
            return 0.0
        return max(0.0, (self._current - self._baseline) / elapsed)

    def remaining_seconds(self):
        """Seconds to completion, floored. 0 when finished, -1 when unknown."""
        if self._finished:
            return 0
        if not self._total:
            return -1
        speed = self.speed_bytes_per_second()
        if speed <= 0:
            return -1
        remaining_bytes = max(0, self._total - self._current)
        return int(remaining_bytes / speed)


def _default_fs_factory():
    # Imported lazily: the tests inject a fake factory and must run without
    # huggingface_hub installed.
    from huggingface_hub import HfFileSystem

    return HfFileSystem(token=os.environ.get("HF_TOKEN") or None)


def fetch_total_bytes(model_id, exclude_patterns, fs_factory=_default_fs_factory):
    """Expected download size in bytes, or None when it cannot be determined.

    Applied the downloader's --exclude patterns so the total reflects only files
    that will actually be fetched.

    Any failure returns None, which surfaces as an ETA of -1. This is called
    once at startup and never retried.
    """
    try:
        fs = fs_factory()
        entries = fs.ls(model_id, detail=True)
        total = 0
        for entry in entries:
            if entry.get("type") != "file":
                continue
            rel = entry["name"]
            prefix = model_id + "/"
            if rel.startswith(prefix):
                rel = rel[len(prefix) :]
            if any(fnmatch.fnmatch(rel, pat) for pat in exclude_patterns):
                continue
            total += entry.get("size") or 0
        return total or None
    except Exception:
        return None


SPEED_METRIC = "model_mirror_download_speed_bytes_per_second"
REMAINING_METRIC = "model_mirror_download_remaining_seconds"

DEFAULT_PORT = 9100
SAMPLE_INTERVAL_SECONDS = 60


def build_registry(state):
    """Registry holding the two download gauges.

    A dedicated registry so the payload carries
    only these two families and none of the python_* / process_* collectors.

    The gauges are backed by callables because serve() builds this registry
    before the sampling loop takes its first reading; a plain set() would pin
    both values at their startup zeros.
    """
    registry = CollectorRegistry()
    Gauge(
        SPEED_METRIC,
        "Average download speed since the download started",
        registry=registry,
    ).set_function(state.speed_bytes_per_second)
    Gauge(
        REMAINING_METRIC,
        "Estimated seconds to completion; -1 when unknown",
        registry=registry,
    ).set_function(state.remaining_seconds)
    return registry


def render_metrics(state):
    """Prometheus text format. The operator truncates toward zero when assigning
    to its int64 status fields."""
    return generate_latest(build_registry(state)).decode()


def serve(state, port=DEFAULT_PORT):
    """Bind and serve /metrics in a daemon thread. Returns the server."""
    server, _thread = start_http_server(port, registry=build_registry(state))
    return server


def install_sigterm_handler():
    """Exit 0 on SIGTERM instead of being SIGKILLed 30s later.

    The kubelet terminates this native sidecar once the downloader exits, on both
    the success and failure paths. Without a handler the container lingers for the
    full terminationGracePeriodSeconds (30s by default) and then reports exit code
    137, which reads as a crash in `kubectl get pod`.

    A handler is required rather than relying on Python's default SIGTERM
    disposition: the container entrypoint uses `exec python3`, so this process is
    PID 1, and the kernel does not deliver default-disposition signals to PID 1.
    """

    def _exit(_signum, _frame):
        sys.exit(0)

    signal.signal(signal.SIGTERM, _exit)


def main():
    install_sigterm_handler()

    model_id = os.environ["MODEL_ID"]
    model_dir = os.path.join("/models", model_id)
    exclude = [p for p in os.environ.get("EXCLUDE_PATTERNS", "").split(",") if p]

    # The baseline is this sidecar's first reading. A Job retry shares
    # the PVC with the previous attempt.
    baseline = directory_bytes(model_dir)
    state = ProgressState(baseline_bytes=baseline, start_time=time.time())

    # Serve before the total-size lookup: that lookup does network I/O and the
    # operator may poll during it. Until the first sample lands the endpoint
    # reports speed 0 / ETA -1, which is a valid "not yet known" answer.
    serve(state)

    state.set_total(fetch_total_bytes(model_id, exclude))

    while True:
        now = time.time()
        state.update(directory_bytes(model_dir), now)
        state.set_finished(is_finished(model_dir))
        time.sleep(SAMPLE_INTERVAL_SECONDS)


if __name__ == "__main__":
    sys.exit(main())
