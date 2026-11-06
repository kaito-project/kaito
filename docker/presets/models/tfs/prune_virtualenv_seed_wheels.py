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

"""Remove virtualenv seed wheels that the image's Python never uses.

virtualenv (pulled in by ray[default]) bundles pip/setuptools seed wheels for
every Python version it supports. Only the wheels mapped to the running
interpreter's major.minor are needed, so the rest are deleted. The wheel set is
read from virtualenv itself rather than hard-coded because virtualenv is an
unpinned transitive dependency.

virtualenv also ships a CycloneDX SBOM in its dist-info that lists every bundled
seed wheel. Entries for the deleted wheels are removed so the SBOM matches the
files actually shipped in the image.
"""

import json
import sys
from pathlib import Path

import virtualenv
from virtualenv.seed.wheels.embed import BUNDLE_FOLDER, BUNDLE_SUPPORT

# Seed wheels that must not remain after pruning (they are only mapped to older
# Python versions in current virtualenv releases).
_MUST_BE_ABSENT = ("pip-26.0.1-", "setuptools-82.0.1-")
_MUST_BE_ABSENT_PURLS = ("pkg:pypi/pip@26.0.1", "pkg:pypi/setuptools@82.0.1")


def prune_sbom_entries() -> int:
    """Drop SBOM components whose bundled seed wheel no longer exists."""
    site_packages = Path(virtualenv.__file__).resolve().parent.parent
    for sbom in sorted(site_packages.glob("virtualenv-*.dist-info/sboms/*.cdx.json")):
        bom = json.loads(sbom.read_text())
        removed = set()
        kept = []
        for component in bom.get("components", []):
            wheel = next(
                (
                    prop["value"]
                    for prop in component.get("properties", [])
                    if prop.get("name") == "virtualenv:bundled-wheel"
                ),
                None,
            )
            if wheel and not (BUNDLE_FOLDER / Path(wheel).name).is_file():
                removed.add(component["bom-ref"])
            else:
                kept.append(component)
        if removed:
            _write_pruned(sbom, bom, kept, removed)

        text = sbom.read_text()
        leftover = [purl for purl in _MUST_BE_ABSENT_PURLS if purl in text]
        if leftover:
            print(f"unexpected SBOM entries remain in {sbom}: {leftover}")
            return 1
    return 0


def _write_pruned(sbom: Path, bom: dict, kept: list, removed: set) -> None:
    def gone(ref: str) -> bool:
        return ref.split("#", 1)[0] in removed

    bom["components"] = kept
    bom["dependencies"] = [
        {**dep, "dependsOn": [r for r in dep.get("dependsOn", []) if not gone(r)]}
        for dep in bom.get("dependencies", [])
        if not gone(dep["ref"])
    ]
    sbom.write_text(json.dumps(bom, indent=2))
    print(f"pruned {sorted(removed)} from {sbom}")


def main() -> int:
    version = f"{sys.version_info.major}.{sys.version_info.minor}"
    mapping = BUNDLE_SUPPORT.get(version)
    if not mapping:
        print(f"virtualenv has no seed wheels mapped for Python {version}")
        return 1

    keep = set(mapping.values())
    missing = sorted(name for name in keep if not (BUNDLE_FOLDER / name).is_file())
    if missing:
        print(f"seed wheels mapped for Python {version} are missing: {missing}")
        return 1

    for wheel in sorted(BUNDLE_FOLDER.glob("*.whl")):
        if wheel.name not in keep:
            wheel.unlink()
            print(f"removed {wheel.name}")

    leftover = sorted(
        wheel.name
        for wheel in BUNDLE_FOLDER.glob("*.whl")
        if wheel.name.startswith(_MUST_BE_ABSENT)
    )
    if leftover:
        print(f"unexpected seed wheels remain: {leftover}")
        return 1

    print(f"kept seed wheels for Python {version}: {sorted(keep)}")
    return prune_sbom_entries()


if __name__ == "__main__":
    sys.exit(main())
