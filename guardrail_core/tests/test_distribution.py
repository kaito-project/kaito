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

import os
import shutil
import subprocess
import sys
import tarfile
import tomllib
import zipfile
from email.parser import BytesParser
from pathlib import Path

import pytest


@pytest.fixture(scope="module")
def distributions(tmp_path_factory):
    root = Path(__file__).resolve().parents[1]
    config = tomllib.loads((root / "pyproject.toml").read_text())
    if dist := os.environ.get("GUARDRAIL_CORE_DIST"):
        return Path(dist), config
    workspace = tmp_path_factory.mktemp("guardrail-distribution")
    source = workspace / "source"
    shutil.copytree(
        root,
        source,
        ignore=shutil.ignore_patterns(
            "build", "dist", "*.egg-info", "__pycache__", ".pytest_cache"
        ),
    )
    subprocess.run(
        [
            sys.executable,
            "-m",
            "build",
            "--outdir",
            str(workspace / "dist"),
            str(source),
        ],
        check=True,
    )
    return workspace / "dist", config


def test_wheel_metadata_and_contents(distributions):
    dist, config = distributions
    wheels = list(dist.glob("*.whl"))
    assert len(wheels) == 1
    with zipfile.ZipFile(wheels[0]) as wheel:
        names = wheel.namelist()
        metadata_name = next(name for name in names if name.endswith("/METADATA"))
        metadata = BytesParser().parsebytes(wheel.read(metadata_name))
        assert metadata["Name"] == "kaito-guardrail-core"
        assert metadata["Version"] == config["project"]["version"]
        assert metadata["Requires-Python"] == ">=3.12"
        assert metadata["License-Expression"] == "Apache-2.0"
        assert set(metadata.get_all("Requires-Dist")) == set(
            config["project"]["dependencies"]
        )
        assert "KAITO Guardrail Core" in metadata.get_payload()
        assert any(name.endswith(".dist-info/licenses/LICENSE") for name in names)
        assert "guardrail_core/streaming/sse.py" in names
        assert "guardrail_core/streaming/openai.py" in names
        assert "guardrail_core/streaming/window.py" in names
        assert "guardrail_core/native_scanners.py" in names
        assert "guardrail_core/config.py" in names
        assert "guardrail_core/secrets/scanner.py" in names
        assert all(
            name.startswith(("guardrail_core/", "kaito_guardrail_core-"))
            for name in names
        )
        assert not any(
            Path(name).name.startswith("test_")
            or "__pycache__" in Path(name).parts
            or "tests" in Path(name).parts
            for name in names
        )


def test_sdist_contains_build_inputs_and_tests(distributions):
    dist, _ = distributions
    archives = list(dist.glob("*.tar.gz"))
    assert len(archives) == 1
    with tarfile.open(archives[0]) as archive:
        names = {name.split("/", 1)[-1] for name in archive.getnames()}
    assert {
        "pyproject.toml",
        "setup.py",
        "MANIFEST.in",
        "README.md",
        "LICENSE",
        "guardrail_core/streaming/sse.py",
        "guardrail_core/secrets/test_scanner.py",
        "tests/streaming/test_sse.py",
        "tests/streaming/test_window.py",
        "guardrail_core/streaming/window.py",
        "guardrail_core/config.py",
        "tests/test_config.py",
        "tests/test_distribution.py",
    } <= names
    assert not any(
        name.startswith(("ragengine/", "presets/", "build/")) for name in names
    )


def test_distribution_licenses(distributions):
    dist, _ = distributions
    expected = (Path(__file__).resolve().parents[1] / "LICENSE").read_bytes()
    with zipfile.ZipFile(next(dist.glob("*.whl"))) as wheel:
        name = next(
            name
            for name in wheel.namelist()
            if name.endswith(".dist-info/licenses/LICENSE")
        )
        assert wheel.read(name) == expected
    with tarfile.open(next(dist.glob("*.tar.gz"))) as archive:
        name = next(name for name in archive.getnames() if name.endswith("/LICENSE"))
        license_file = archive.extractfile(name)
        assert license_file is not None
        assert license_file.read() == expected
