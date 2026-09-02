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
import unittest
from unittest import mock

import model_warmer


class SelectWeightFilesTest(unittest.TestCase):
    def test_stripes_sorted_files_across_partitions(self):
        weight_files = [
            "model-00004.safetensors",
            "model-00001.safetensors",
            "model-00003.safetensors",
            "model-00002.safetensors",
            "model-00005.safetensors",
        ]

        self.assertEqual(
            model_warmer.select_weight_files(weight_files, 0, 2),
            [
                "model-00001.safetensors",
                "model-00003.safetensors",
                "model-00005.safetensors",
            ],
        )
        self.assertEqual(
            model_warmer.select_weight_files(weight_files, 1, 2),
            [
                "model-00002.safetensors",
                "model-00004.safetensors",
            ],
        )

    def test_rejects_invalid_partition(self):
        with self.assertRaisesRegex(ValueError, "outside"):
            model_warmer.select_weight_files(["model.safetensors"], 2, 2)


class WarmerPartitionTest(unittest.TestCase):
    @mock.patch.dict(
        os.environ,
        {"KAITO_MODEL_WARMER_PARTITION_COUNT": "3"},
        clear=True,
    )
    def test_uses_pod_ordinal_when_partitioning_is_enabled(self):
        self.assertEqual(model_warmer.warmer_partition("model-2"), (2, 3))

    @mock.patch.dict(os.environ, {}, clear=True)
    def test_defaults_to_ordinal_zero_only(self):
        self.assertEqual(model_warmer.warmer_partition("model-0"), (0, 1))
        self.assertIsNone(model_warmer.warmer_partition("model-1"))

    @mock.patch.dict(
        os.environ,
        {"KAITO_MODEL_WARMER_PARTITION_COUNT": "2"},
        clear=True,
    )
    def test_rejects_ordinal_outside_partition_count(self):
        with self.assertRaisesRegex(ValueError, "outside"):
            model_warmer.warmer_partition("model-2")


class WaitForCacheTest(unittest.TestCase):
    @mock.patch.dict(
        os.environ,
        {
            "CACHE_DISCOVERY_URL": "cache.example.test",
            "CACHE_SERVER_PORT": "9065",
        },
        clear=True,
    )
    @mock.patch.object(model_warmer, "log")
    @mock.patch.object(model_warmer.socket, "create_connection")
    def test_returns_when_cache_endpoint_is_reachable(self, create_connection, log):
        model_warmer.wait_for_cache()

        create_connection.assert_called_once_with(
            ("cache.example.test", 9065),
            timeout=model_warmer._CACHE_CONNECT_TIMEOUT_SECONDS,
        )
        log.assert_any_call("cache endpoint is reachable cache.example.test:9065")

    @mock.patch.dict(
        os.environ,
        {
            "CACHE_DISCOVERY_URL": "cache.example.test",
            "CACHE_SERVER_PORT": "9065",
        },
        clear=True,
    )
    @mock.patch.object(model_warmer, "log")
    @mock.patch.object(model_warmer.time, "sleep")
    @mock.patch.object(model_warmer.socket, "create_connection")
    def test_retries_until_cache_endpoint_is_reachable(
        self,
        create_connection,
        sleep,
        _log,
    ):
        create_connection.side_effect = [
            ConnectionRefusedError,
            TimeoutError,
            mock.MagicMock(),
        ]

        model_warmer.wait_for_cache()

        self.assertEqual(create_connection.call_count, 3)
        self.assertEqual(
            sleep.call_args_list,
            [
                mock.call(model_warmer._CACHE_RETRY_INTERVAL_SECONDS),
                mock.call(model_warmer._CACHE_RETRY_INTERVAL_SECONDS),
            ],
        )


if __name__ == "__main__":
    unittest.main()
