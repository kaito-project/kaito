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

"""Offline integration of pinned HF local-dir downloads and EXDEV copy fallback."""

import errno
from pathlib import Path

import huggingface_hub
import pytest
from huggingface_hub import file_download, hf_hub_download
from sampler import has_incomplete


@pytest.mark.parametrize("existing_destination", [False, True])
def test_local_cache_cross_filesystem_download(
    tmp_path, monkeypatch, existing_destination
):
    assert huggingface_hub.__version__ == "2.3.0"
    model_dir = tmp_path / "models" / "org" / "model"
    model_dir.mkdir(parents=True)
    destination = model_dir / "model.safetensors"
    if existing_destination:
        destination.write_bytes(b"old weights")
    cache = model_dir / ".cache" / "huggingface"
    content = b"offline model weights"
    commit_hash = "a" * 40
    monkeypatch.setattr(
        file_download,
        "_get_metadata_or_catch_error",
        lambda **kwargs: (
            "https://offline.invalid/weights",
            "etag",
            commit_hash,
            len(content),
            None,
            None,
        ),
    )

    def fake_http_get(url, target, **kwargs):
        assert url == "https://offline.invalid/weights"
        assert Path(target.name).is_relative_to(cache)
        assert str(target.name).endswith(".incomplete")
        assert has_incomplete(str(model_dir)), "the sampler must see local staging"
        target.write(content)

    monkeypatch.setattr(file_download, "http_get", fake_http_get)
    real_rename = file_download.os.rename
    cross_device_moves = []

    def cross_device_rename(src, dst, *args, **kwargs):
        if Path(src).is_relative_to(cache) and Path(dst) == destination:
            cross_device_moves.append((src, dst))
            raise OSError(errno.EXDEV, "Invalid cross-device link")
        return real_rename(src, dst, *args, **kwargs)

    monkeypatch.setattr(file_download.os, "rename", cross_device_rename)
    result = hf_hub_download(
        repo_id="org/model",
        filename=destination.name,
        local_dir=model_dir,
        cache_dir=tmp_path / "unused-hub-cache",
        force_download=True,
    )
    assert Path(result) == destination
    assert destination.read_bytes() == content
    assert len(cross_device_moves) == 1, "exercise the real shutil.move copy fallback"
    assert not has_incomplete(str(model_dir))
    metadata = cache / "download" / "model.safetensors.metadata"
    assert metadata.is_file()
    assert commit_hash in metadata.read_text()
    assert cache.is_dir()
