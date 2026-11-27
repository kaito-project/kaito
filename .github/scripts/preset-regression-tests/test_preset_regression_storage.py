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

"""Check preset storage selection and disk guards without cluster access."""

import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

from preset_regression_test_utils import load_yaml

ROOT = Path(__file__).resolve().parents[3]
RUNNER = (
    Path(__file__).with_name("preset-regression-run.sh").read_text(encoding="utf-8")
)
STUBS = """
helm() { test "${HELM_STATUS:-0}" = 0 && export PREPARED=true; }
log() { echo "$*" >&2; }
kubectl() {
  case "$1 $2" in
    "get node")
      jq -n --arg value "$NODE_LABEL" \
        '{metadata:{labels:{"dacs.azure.com/processing-unit":$value}}}';;
    "create -f") test "$PREPARED" = true && cat;;
    *) return 99;;
  esac
}
"""


def run_function(name, manifest, **env):
    """Run the actual shell function, replacing every cluster command."""
    function = re.search(rf"(?ms)^{name}\(\) \{{\n.*?^\}}", RUNNER)[0]
    script = (
        STUBS + function + f'\n{name} "$1" 60\nprintf "%s" "${{storage_enabled:-}}"'
    )
    command = ["bash", "-euo", "pipefail", "-c", script, "test", str(manifest)]
    return subprocess.run(
        command,
        cwd=ROOT,
        env={**os.environ, "DACS_CHART": "fixture.tgz", **env},
        capture_output=True,
        text=True,
        check=False,
    )


class StorageRoutingTest(unittest.TestCase):
    """Cover routing and fail-closed disk preparation."""

    def test_streaming_and_opt_out(self):
        """Only streaming presets retain DACS attachment and disk ownership."""
        for enabled, opt_out in (("true", False), ("false", False), ("true", True)):
            with (
                self.subTest(enabled=enabled, opt_out=opt_out),
                tempfile.TemporaryDirectory(dir=ROOT) as directory,
            ):
                manifest = Path(directory) / "workspace.yaml"
                manifest.write_text(f"""\
metadata:
  annotations:
    kaito.sh/model-streaming: {"disabled" if opt_out else "enabled"}
    kaito.sh/cache-config: old-cache
resource:
  labelSelector:
    matchLabels:
      dacs.azure.com/processing-unit: cpu
""")
                result = run_function(
                    "configure_storage", manifest, STREAMING_ENABLED=enabled
                )
                self.assertEqual(0, result.returncode, result.stderr)
                expected = enabled == "true" and not opt_out
                self.assertEqual(str(expected).lower(), result.stdout)
                value = load_yaml(manifest)
                annotations = value["metadata"]["annotations"]
                labels = value["resource"]["labelSelector"]["matchLabels"]
                self.assertEqual(expected, "kaito.sh/cache-config" in annotations)
                self.assertEqual(expected, "dacs.azure.com/processing-unit" in labels)

    def test_cache_waits_for_owned_nodes_and_disk_preparation(self):
        """Empty, unowned and failed-initialization nodes never create a Cache."""
        for nodes, label, exit_code, expected in (
            ([], "cpu", "0", False),
            (["gpu"], "unowned", "0", False),
            (["gpu"], "cpu", "1", False),
            (["gpu"], "cpu", "0", True),
        ):
            with (
                self.subTest(nodes=nodes, label=label, exit_code=exit_code),
                tempfile.TemporaryDirectory() as directory,
            ):
                manifest = Path(directory) / "workspace.json"
                workspace = {"metadata": {"uid": "test-uid"}}
                workspace["status"] = {"workerNodes": nodes}
                manifest.write_text(json.dumps(workspace))
                result = run_function(
                    "prepare_streaming_storage",
                    manifest,
                    NODE_LABEL=label,
                    HELM_STATUS=exit_code,
                )
                self.assertEqual(expected, result.returncode == 0, result.stderr)
                if expected:
                    self.assertEqual(
                        "test-uid",
                        json.loads(result.stdout)["metadata"]["labels"][
                            "kaito.sh/preset-regression"
                        ],
                    )
                else:
                    self.assertEqual("", result.stdout)


if __name__ == "__main__":
    unittest.main()
