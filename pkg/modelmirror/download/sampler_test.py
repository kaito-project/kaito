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

"""Unit tests for sampler.py.

NOTE: this file is under a Go package dir, so CI pytest globs (which target
presets/) do NOT run it automatically. Run manually during development:
    python3 pkg/modelmirror/download/sampler_test.py
"""

import importlib.util
import os
import sys
import tempfile

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "sampler", os.path.join(_here, "sampler.py")
)
sampler = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(sampler)


def _make_model_dir(root, files, incomplete=()):
    """Build a fixture directory. files/incomplete are (relpath, size) pairs."""
    for rel, size in files:
        path = os.path.join(root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write(b"\0" * size)
    for rel, size in incomplete:
        path = os.path.join(root, ".cache", "huggingface", "download", rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write(b"\0" * size)


def test_directory_bytes_sums_regular_files():
    with tempfile.TemporaryDirectory() as d:
        _make_model_dir(d, [("a.bin", 100), ("sub/b.bin", 50)])
        assert sampler.directory_bytes(d) == 150


def test_directory_bytes_skips_symlinks():
    with tempfile.TemporaryDirectory() as d:
        _make_model_dir(d, [("a.bin", 100)])
        os.symlink(os.path.join(d, "a.bin"), os.path.join(d, "link.bin"))
        # A symlink would otherwise double-count the file it points at.
        assert sampler.directory_bytes(d) == 100


def test_directory_bytes_missing_dir_is_zero():
    assert sampler.directory_bytes("/nonexistent/path/xyz") == 0


def test_has_incomplete_true_when_present():
    with tempfile.TemporaryDirectory() as d:
        _make_model_dir(d, [("a.bin", 10)], incomplete=[("a.bin.incomplete", 5)])
        assert sampler.has_incomplete(d) is True


def test_has_incomplete_false_when_absent():
    with tempfile.TemporaryDirectory() as d:
        _make_model_dir(d, [("a.bin", 10)])
        assert sampler.has_incomplete(d) is False


def test_is_finished_requires_bytes_and_no_incomplete():
    with tempfile.TemporaryDirectory() as d:
        # t=0: the downloader has not created any .incomplete files yet. Testing
        # only for their absence would report "finished" before the first byte.
        assert sampler.is_finished(d) is False
        _make_model_dir(d, [("a.bin", 10)], incomplete=[("a.bin.incomplete", 5)])
        assert sampler.is_finished(d) is False
        os.remove(
            os.path.join(d, ".cache", "huggingface", "download", "a.bin.incomplete")
        )
        assert sampler.is_finished(d) is True


def test_speed_is_growth_since_baseline_not_total_bytes():
    # A Job retry starts against a PVC that already holds the previous attempt's
    # bytes (BackoffLimit is 3 and every attempt shares one PVC). Treating the
    # pre-existing bytes as downloaded would divide them by a few seconds of
    # elapsed time and report a wildly inflated speed.
    s = sampler.ProgressState(baseline_bytes=1_000_000, start_time=100.0)
    s.update(current_bytes=1_500_000, now=110.0)
    assert s.speed_bytes_per_second() == 50_000.0


def test_speed_is_zero_before_any_elapsed_time():
    s = sampler.ProgressState(baseline_bytes=0, start_time=100.0)
    s.update(current_bytes=0, now=100.0)
    assert s.speed_bytes_per_second() == 0.0


def test_speed_is_cumulative_not_windowed():
    # Deliberately differs from the rolling window in inference_api.py: the
    # average spans the whole run, so a brief stall does not swing the number.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0)
    s.update(current_bytes=100, now=10.0)  # 10 B/s
    s.update(current_bytes=100, now=20.0)  # stalled; cumulative -> 5 B/s
    assert s.speed_bytes_per_second() == 5.0


def test_within_pod_resumption_is_invisible_to_the_sampler():
    # The sampler measures the filesystem, not the downloader process, so a
    # crashed and restarted `hf download` needs no special handling: the byte
    # count simply continues climbing from wherever the partial file left off.
    # No sample is discarded and the baseline does not move.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0)
    s.update(current_bytes=100, now=10.0)
    s.update(current_bytes=100, now=20.0)  # downloader died, nothing written
    s.update(current_bytes=400, now=40.0)  # restarted, resumed from 100
    assert s.speed_bytes_per_second() == 10.0


def test_eta_from_total_and_speed():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=1000)
    s.update(current_bytes=400, now=10.0)  # 40 B/s, 600 remaining
    assert s.remaining_seconds() == 15


def test_eta_unknown_when_total_unknown():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=None)
    s.update(current_bytes=400, now=10.0)
    assert s.remaining_seconds() == -1


def test_eta_unknown_when_speed_zero():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=1000)
    s.update(current_bytes=0, now=10.0)
    assert s.remaining_seconds() == -1


def test_eta_clamps_when_bytes_exceed_total():
    # The Hub total can over-count when a repo ships duplicate weight formats and
    # --exclude drops one set; never report a negative ETA.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=100)
    s.update(current_bytes=500, now=10.0)
    assert s.remaining_seconds() == 0


def test_finished_reports_zero_for_both():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=1000)
    s.update(current_bytes=1000, now=10.0)
    s.set_finished(True)
    assert s.speed_bytes_per_second() == 0.0
    assert s.remaining_seconds() == 0


def test_finished_is_not_a_latch():
    # Each worker in snapshot_download's thread pool holds no .incomplete file
    # between renaming one file and opening the next. When every worker is in
    # that gap at once, a sample sees zero .incomplete mid-download. If
    # "finished" latched, that one transient sample would freeze 0/0 in status
    # for the rest of the run.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=1000)
    s.update(current_bytes=100, now=10.0)
    s.set_finished(True)  # transient: no .incomplete visible this sample
    assert s.speed_bytes_per_second() == 0.0

    s.update(current_bytes=500, now=50.0)
    s.set_finished(False)  # next sample sees .incomplete again
    assert s.speed_bytes_per_second() == 10.0
    assert s.remaining_seconds() == 50


def test_set_total_updates_the_eta():
    # main() starts serving before resolving the total, then fills it in.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0)
    s.update(current_bytes=400, now=10.0)
    assert s.remaining_seconds() == -1
    s.set_total(1000)
    assert s.remaining_seconds() == 15


def test_initial_state_before_first_sample():
    # The HTTP server binds before the first sample lands; an early operator
    # fetch must get a valid response, not a connection error or a crash.
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0)
    assert s.speed_bytes_per_second() == 0.0
    assert s.remaining_seconds() == -1


class _FakeHfFileSystem:
    def __init__(self, entries):
        self._entries = entries

    def ls(self, _repo, detail=True):
        return self._entries


def test_total_bytes_sums_files_only():
    fs = _FakeHfFileSystem(
        [
            {"name": "m/a.bin", "size": 100, "type": "file"},
            {"name": "m/sub", "size": 0, "type": "directory"},
            {"name": "m/b.bin", "size": 50, "type": "file"},
        ]
    )
    assert sampler.fetch_total_bytes("m", ["original/*"], fs_factory=lambda: fs) == 150


def test_total_bytes_applies_exclude_patterns():
    # The downloader passes the same --exclude patterns, so a total that counted
    # excluded files would make the ETA permanently unreachable.
    fs = _FakeHfFileSystem(
        [
            {"name": "m/a.bin", "size": 100, "type": "file"},
            {"name": "m/original/consolidated.pth", "size": 900, "type": "file"},
        ]
    )
    assert sampler.fetch_total_bytes("m", ["original/*"], fs_factory=lambda: fs) == 100


def test_total_bytes_none_on_error():
    # A missing ETA must never stall or fail a download.
    def boom():
        raise RuntimeError("network down")

    assert sampler.fetch_total_bytes("m", [], fs_factory=boom) is None


def test_total_bytes_none_when_zero():
    fs = _FakeHfFileSystem([])
    assert sampler.fetch_total_bytes("m", [], fs_factory=lambda: fs) is None


def test_render_metrics_format():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0, total_bytes=1000)
    s.update(current_bytes=400, now=10.0)
    out = sampler.render_metrics(s)
    assert "# TYPE model_mirror_download_speed_bytes_per_second gauge" in out
    assert "# TYPE model_mirror_download_remaining_seconds gauge" in out
    assert "model_mirror_download_speed_bytes_per_second 40" in out
    assert "model_mirror_download_remaining_seconds 15" in out
    assert out.endswith("\n")


def test_render_metrics_unknown_eta():
    s = sampler.ProgressState(baseline_bytes=0, start_time=0.0)
    out = sampler.render_metrics(s)
    assert "model_mirror_download_remaining_seconds -1" in out


def test_speed_never_negative_when_bytes_shrink():
    # The job script's post-download cleanup deletes .cache and every empty
    # subdirectory, so a sample can read fewer bytes than the baseline. A
    # negative speed would reach the operator's int64 status field unclamped.
    s = sampler.ProgressState(baseline_bytes=1000, start_time=0.0)
    s.update(current_bytes=200, now=10.0)
    assert s.speed_bytes_per_second() == 0.0
    assert "-" not in sampler.render_metrics(s).split("speed_bytes_per_second ")[1]


def test_sigterm_handler_exits_zero():
    # The kubelet SIGTERMs this sidecar when the downloader exits. Without a
    # handler the process is PID 1 (the entrypoint uses `exec python3`), the
    # kernel drops the default-disposition signal, and the container is SIGKILLed
    # after the grace period -- surfacing as a spurious exit code 137.
    import signal as signal_mod

    previous = signal_mod.getsignal(signal_mod.SIGTERM)
    try:
        sampler.install_sigterm_handler()
        handler = signal_mod.getsignal(signal_mod.SIGTERM)
        assert callable(handler), "SIGTERM left at default disposition"
        raised = False
        try:
            handler(signal_mod.SIGTERM, None)
        except SystemExit as e:
            raised = True
            assert e.code == 0, f"exited {e.code}, want 0"
        assert raised, "handler did not exit"
    finally:
        signal_mod.signal(signal_mod.SIGTERM, previous)


if __name__ == "__main__":
    import traceback

    failures = 0
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            try:
                fn()
                print(f"PASS {name}")
            except Exception:
                failures += 1
                print(f"FAIL {name}")
                traceback.print_exc()
    sys.exit(1 if failures else 0)
