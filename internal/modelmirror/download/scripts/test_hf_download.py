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

"""Offline integration tests against huggingface-hub==2.3.0 and native filelock."""

import hashlib
import importlib.util
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from unittest.mock import patch

import huggingface_hub
import pytest
from filelock import FileLock, Timeout
from huggingface_hub import _local_folder, file_download, utils
from huggingface_hub.utils import _fixes

spec = importlib.util.spec_from_file_location(
    "hf_download", Path(__file__).with_name("hf_download.py")
)
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


@pytest.fixture
def isolated(tmp_path):
    # Deliberately import HF consumers before installation to test cached aliases.
    original = _fixes.WeakFileLock
    model = tmp_path / "models" / "org" / "model"
    locks = tmp_path / "hf-locks"
    helper.install_local_locks(model, locks)
    yield model, locks
    for name, module in list(sys.modules.items()):
        if name.startswith("huggingface_hub.") and hasattr(module, "WeakFileLock"):
            module.WeakFileLock = original


def test_pin_matches_job():
    job_constants = Path(__file__).parents[1] / "resources.go"
    assert (
        f'HuggingFaceHubVersion = "{helper.SUPPORTED_HF_VERSION}"'
        in job_constants.read_text()
    )
    assert huggingface_hub.__version__ == helper.SUPPORTED_HF_VERSION
    assert (
        f'FileLockVersion = "{helper.SUPPORTED_FILELOCK_VERSION}"'
        in job_constants.read_text()
    )


def test_aliases_paths_and_metadata(isolated):
    model, locks = isolated
    assert _local_folder.WeakFileLock is _fixes.WeakFileLock
    assert file_download.WeakFileLock is utils.WeakFileLock is _fixes.WeakFileLock
    paths = _local_folder.get_local_download_paths(model, "nested/model.safetensors")
    original = paths.lock_path
    expected = locks / (hashlib.sha256(os.fsencode(original)).hexdigest() + ".lock")
    with _local_folder.WeakFileLock(original) as lock:
        assert Path(lock.lock_file) == expected
        assert expected.exists()
        assert not original.exists()
        paths.incomplete_path("etag").write_bytes(b"payload")
        paths.file_path.write_bytes(b"model")
    _local_folder.write_download_metadata(
        model, "nested/model.safetensors", "commit", "etag"
    )
    assert paths.metadata_path.is_relative_to(model)
    assert paths.metadata_path.exists()
    assert paths.incomplete_path("etag").is_relative_to(model)
    assert paths.incomplete_path("etag").read_bytes() == b"payload"
    assert all(p.suffix == ".lock" for p in locks.iterdir())
    assert expected.exists()  # Never unlink a file other workers may be waiting on.
    assert not list(model.rglob("*.lock"))
    with utils.WeakFileLock(original) as lock:
        assert lock.lock_file == str(expected)
    with utils.WeakFileLock(original.parent / "different.lock") as lock:
        assert lock.lock_file != str(expected)


def test_unrelated_locks_are_not_redirected(isolated, tmp_path):
    model, locks = isolated
    for path in [tmp_path / "outside.lock", model / "other.lock"]:
        with utils.WeakFileLock(path) as lock:
            assert lock.lock_file == str(path)


def test_no_pvc_lock_operations(isolated):
    model, locks = isolated
    original_unlink = Path.unlink

    def unlink(path, *args, **kwargs):
        assert not (path.is_relative_to(model) and path.suffix == ".lock")
        return original_unlink(path, *args, **kwargs)

    with patch.object(Path, "unlink", unlink):
        paths = _local_folder.get_local_download_paths(model, "weights.bin")
        with utils.WeakFileLock(paths.lock_path) as lock:
            assert Path(lock.lock_file).parent == locks
        _local_folder.write_download_metadata(model, "weights.bin", "commit", "etag")


def test_threads_processes_and_exception_release(isolated):
    model, locks = isolated
    path = model / ".cache/huggingface/download/weights.lock"

    def contend():
        with utils.WeakFileLock(path, timeout=0.1):
            pytest.fail("overlapping worker acquired the lock")

    with utils.WeakFileLock(path) as lock:
        with ThreadPoolExecutor(max_workers=1) as pool, pytest.raises(Timeout):
            pool.submit(contend).result()
        code = """
import sys
from filelock import FileLock, Timeout
try:
    with FileLock(sys.argv[1], timeout=0.1, fallback_to_soft=False):
        sys.exit(1)
except Timeout:
    sys.exit(0)
"""
        subprocess.run([sys.executable, "-c", code, lock.lock_file], check=True)
    with pytest.raises(ValueError), utils.WeakFileLock(path):
        raise ValueError("download failed")
    with utils.WeakFileLock(path, timeout=0.1):
        pass


def test_native_failure_is_not_soft_fallback(isolated):
    model, locks = isolated
    path = model / ".cache/huggingface/download/weights.lock"
    with (
        patch.object(
            FileLock,
            "_acquire",
            side_effect=NotImplementedError("use SoftFileLock instead"),
        ),
        pytest.raises(NotImplementedError),
        utils.WeakFileLock(path),
    ):
        pytest.fail("must not enter an unprotected download")
    with patch.object(FileLock, "_release", side_effect=OSError("release failed")):
        with pytest.raises(OSError), utils.WeakFileLock(path) as lock:
            pass
        assert Path(lock.lock_file).exists()
        assert not path.exists()
    lock.release(force=True)


def test_unvalidated_version_fails_closed(tmp_path, monkeypatch):
    monkeypatch.setattr(huggingface_hub, "__version__", "99.0.0")
    with pytest.raises(RuntimeError, match="Revalidate"):
        helper.install_local_locks(tmp_path / "model", tmp_path / "locks")


def test_actual_local_download_keeps_payload_and_probes_separate(isolated, monkeypatch):
    model, locks = isolated
    opened = []
    original_open = os.open
    original_unlink = os.unlink

    def checked_open(path, *args, **kwargs):
        path = Path(path)
        opened.append(path)
        # filelock's native-lock probe and lock creation both use os.open.
        assert path.is_relative_to(locks), f"lock/probe outside local disk: {path}"
        return original_open(path, *args, **kwargs)

    def checked_unlink(path, *args, **kwargs):
        path = Path(path)
        assert not (
            path.is_relative_to(model)
            and (path.suffix == ".lock" or ".filelock-probe" in path.name)
        )
        return original_unlink(path, *args, **kwargs)

    def download(**kwargs):
        assert kwargs["incomplete_path"].is_relative_to(model / ".cache")
        assert kwargs["destination_path"].is_relative_to(model)
        kwargs["incomplete_path"].write_bytes(b"resumable payload")
        kwargs["incomplete_path"].replace(kwargs["destination_path"])

    monkeypatch.setattr(os, "open", checked_open)
    monkeypatch.setattr(os, "unlink", checked_unlink)
    monkeypatch.setattr(
        file_download,
        "_get_metadata_or_catch_error",
        lambda **kw: (
            "https://example.invalid/weights",
            "etag",
            "a" * 40,
            17,
            None,
            None,
        ),
    )
    monkeypatch.setattr(file_download, "_download_to_tmp_and_move", download)
    result = file_download._hf_hub_download_to_local_dir(
        local_dir=model,
        repo_id="org/model",
        repo_type="model",
        filename="nested/weights.safetensors",
        revision="main",
        endpoint=None,
        etag_timeout=10,
        headers={},
        token=None,
        cache_dir=str(model / "unused"),
        force_download=True,
        local_files_only=False,
        tqdm_class=None,
        dry_run=False,
    )
    assert Path(result).read_bytes() == b"resumable payload"
    metadata = _local_folder.read_download_metadata(model, "nested/weights.safetensors")
    assert metadata.etag == "etag"
    assert opened
    assert not list(model.rglob("*.lock"))
    assert not list(model.rglob("*.filelock-probe*"))


def test_timeout_semantics_and_failed_acquisition_do_not_touch_pvc(isolated):
    model, locks = isolated
    path = model / ".cache/huggingface/download/weights.lock"
    with patch.object(FileLock, "acquire", autospec=True) as acquire:
        for timeout, expected in [(None, -1), (0, 0), (0.25, 0.25), (-1, -1)]:
            with utils.WeakFileLock(path, timeout=timeout):
                pass
            assert acquire.call_args.kwargs["timeout"] == expected
    # Regression: HF WeakFileLock used to run original-path cleanup on EIO.
    with (
        patch.object(FileLock, "acquire", side_effect=OSError("probe unlink EIO")),
        patch.object(Path, "unlink", side_effect=AssertionError("must not unlink")),
        pytest.raises(OSError, match="probe unlink EIO"),
        utils.WeakFileLock(path),
    ):
        pytest.fail("failed acquisition must not enter the critical section")
