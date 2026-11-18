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

import subprocess
import unittest
from pathlib import Path

import yaml

CHART = Path(__file__).resolve().parents[1] / "charts/kaito/workspace"
DEVICE_PLUGIN = "nvidia-device-plugin-daemonset"
SELECTOR = {"name": "nvidia-device-plugin-ds"}


def render(values):
    result = subprocess.run(
        [
            "helm",
            "template",
            "workspace",
            str(CHART),
            "--namespace",
            "kaito-workspace",
            "--values",
            "-",
        ],
        input=yaml.safe_dump(values),
        capture_output=True,
        text=True,
        check=True,
    )
    return {
        (doc["kind"], doc["metadata"]["name"]): doc
        for doc in yaml.safe_load_all(result.stdout)
        if doc is not None
    }


class WorkspaceChartLabelsTest(unittest.TestCase):
    def test_default_device_plugin_labels(self):
        plugin = render({})[("DaemonSet", DEVICE_PLUGIN)]
        self.assertEqual(plugin["spec"]["selector"]["matchLabels"], SELECTOR)
        self.assertEqual(plugin["spec"]["template"]["metadata"]["labels"], SELECTOR)
        self.assertEqual(
            plugin["metadata"]["labels"]["app.kubernetes.io/managed-by"], "Helm"
        )

    def test_null_pod_labels_preserve_defaults(self):
        documents = render({"podTemplateLabels": None})
        plugin = documents[("DaemonSet", DEVICE_PLUGIN)]
        self.assertEqual(plugin["spec"]["selector"]["matchLabels"], SELECTOR)
        self.assertEqual(plugin["spec"]["template"]["metadata"]["labels"], SELECTOR)
        controller = documents[("Deployment", "workspace")]
        self.assertEqual(
            controller["spec"]["template"]["metadata"]["labels"],
            controller["spec"]["selector"]["matchLabels"],
        )

    def test_custom_labels_reach_controller_and_pods(self):
        controller_labels = {"example.com/owner": "platform"}
        pod_labels = {
            "example.com/owner": "platform",
            "example.com/revision": 42,
            "example.com/enabled": True,
        }
        documents = render(
            {"commonLabels": controller_labels, "podTemplateLabels": pod_labels}
        )
        plugin = documents[("DaemonSet", DEVICE_PLUGIN)]
        self.assertEqual(plugin["metadata"]["labels"], controller_labels)
        self.assertEqual(plugin["spec"]["selector"]["matchLabels"], SELECTOR)
        expected = {
            "example.com/owner": "platform",
            "example.com/revision": "42",
            "example.com/enabled": "true",
        }
        self.assertEqual(
            plugin["spec"]["template"]["metadata"]["labels"], SELECTOR | expected
        )
        controller = documents[("Deployment", "workspace")]
        self.assertEqual(controller["metadata"]["labels"], controller_labels)
        self.assertEqual(
            controller["spec"]["template"]["metadata"]["labels"],
            controller["spec"]["selector"]["matchLabels"] | expected,
        )

    def test_common_labels_do_not_become_pod_labels(self):
        labels = {"example.com/owner": "platform"}
        plugin = render({"commonLabels": labels})[("DaemonSet", DEVICE_PLUGIN)]
        self.assertEqual(plugin["metadata"]["labels"], labels)
        self.assertEqual(plugin["spec"]["template"]["metadata"]["labels"], SELECTOR)

    def test_pod_labels_cannot_override_device_plugin_selector_label(self):
        documents = render(
            {
                "podTemplateLabels": {
                    "name": "custom-name",
                    "example.com/owner": "platform",
                }
            }
        )
        plugin = documents[("DaemonSet", DEVICE_PLUGIN)]
        self.assertEqual(plugin["spec"]["selector"]["matchLabels"], SELECTOR)
        self.assertEqual(
            plugin["spec"]["template"]["metadata"]["labels"],
            SELECTOR | {"example.com/owner": "platform"},
        )
        controller = documents[("Deployment", "workspace")]
        self.assertEqual(
            controller["spec"]["template"]["metadata"]["labels"]["name"], "custom-name"
        )

    def test_disabled_device_plugin_is_not_rendered(self):
        documents = render(
            {
                "nvidiaDevicePlugin": {"enabled": False},
                "podTemplateLabels": {"example.com/owner": "platform"},
            }
        )
        self.assertNotIn(("DaemonSet", DEVICE_PLUGIN), documents)


if __name__ == "__main__":
    unittest.main()
