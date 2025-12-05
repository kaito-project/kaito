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

"""
Unit tests for lifecycle hooks manager.
Tests the PostStart and PreStop handlers for RAGEngine persistence.
"""

import json
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))

from manager import (
    get_indexes,
    load_index,
    persist_index,
    poststart_handler,
    prestop_handler,
    wait_for_service,
)


class TestLifecycleManager(unittest.TestCase):
    """Test lifecycle manager functions."""

    def setUp(self):
        """Create temporary directory for tests."""
        self.test_dir = tempfile.mkdtemp()
        self.base_path = Path(self.test_dir)

    def tearDown(self):
        """Clean up temporary directory."""
        shutil.rmtree(self.test_dir, ignore_errors=True)

    @patch("manager.requests.get")
    def test_wait_for_service_success(self, mock_get):
        """Test wait_for_service succeeds."""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_get.return_value = mock_response

        result = wait_for_service(timeout=10)

        self.assertTrue(result)
        mock_get.assert_called()

    @patch("manager.requests.get")
    def test_wait_for_service_timeout(self, mock_get):
        """Test wait_for_service times out."""
        mock_get.side_effect = Exception("Connection refused")

        result = wait_for_service(timeout=2)

        self.assertFalse(result)

    @patch("manager.requests.get")
    def test_get_indexes_success(self, mock_get):
        """Test get_indexes returns list of indexes."""
        mock_response = MagicMock()
        mock_response.json.return_value = ["index1", "index2", "index3"]
        mock_get.return_value = mock_response

        result = get_indexes()

        self.assertEqual(result, ["index1", "index2", "index3"])

    @patch("manager.requests.get")
    def test_get_indexes_failure(self, mock_get):
        """Test get_indexes handles errors gracefully."""
        mock_get.side_effect = Exception("Connection failed")

        result = get_indexes()

        self.assertEqual(result, [])

    @patch("manager.requests.post")
    def test_load_index_success(self, mock_post):
        """Test load_index succeeds."""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_post.return_value = mock_response

        result = load_index("test_index", "/path/to/index")

        self.assertTrue(result)
        mock_post.assert_called_once()

    @patch("manager.requests.post")
    def test_load_index_failure(self, mock_post):
        """Test load_index handles errors."""
        mock_post.side_effect = Exception("Load failed")

        result = load_index("test_index", "/path/to/index")

        self.assertFalse(result)

    @patch("manager.requests.post")
    def test_persist_index_success(self, mock_post):
        """Test persist_index succeeds."""
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_post.return_value = mock_response

        result = persist_index("test_index", "/path/to/save")

        self.assertTrue(result)
        mock_post.assert_called_once()

    @patch("manager.requests.post")
    def test_persist_index_failure(self, mock_post):
        """Test persist_index handles errors."""
        mock_post.side_effect = Exception("Persist failed")

        result = persist_index("test_index", "/path/to/save")

        self.assertFalse(result)

    @patch("manager.wait_for_service")
    def test_poststart_no_snapshots(self, mock_wait):
        """Test poststart_handler with no existing snapshots."""
        mock_wait.return_value = True

        result = poststart_handler(base_dir=str(self.base_path))

        self.assertEqual(result, 0)
        mock_wait.assert_called_once()

    @patch("manager.wait_for_service")
    @patch("manager.load_index")
    def test_poststart_with_snapshot(self, mock_load, mock_wait):
        """Test poststart_handler restores indexes from snapshot."""
        mock_wait.return_value = True
        mock_load.return_value = True

        # Create mock snapshot
        snapshots_dir = self.base_path / "systemsnapshots"
        snapshot = snapshots_dir / "2024-11-30T10-00-00_pod-abc123"
        snapshot.mkdir(parents=True)

        # Create metadata
        metadata = {
            "index_names": ["index1", "index2"],
            "timestamp": "2024-11-30T10:00:00",
            "pod_uid": "abc123",
        }
        with open(snapshot / "metadata.json", "w") as f:
            json.dump(metadata, f)

        # Create LATEST symlink
        latest_link = self.base_path / "LATEST"
        latest_link.symlink_to(snapshot.relative_to(self.base_path))

        # Create dummy index directories
        for idx in ["index1", "index2"]:
            (snapshot / idx).mkdir()
            (snapshot / idx / "docstore.json").write_text("{}")

        result = poststart_handler(base_dir=str(self.base_path))

        self.assertEqual(result, 0)
        self.assertEqual(mock_load.call_count, 2)

    @patch("manager.wait_for_service")
    def test_poststart_service_not_ready(self, mock_wait):
        """Test poststart_handler when service doesn't become ready."""
        mock_wait.return_value = False

        result = poststart_handler(base_dir=str(self.base_path))

        self.assertEqual(result, 1)

    @patch("manager.get_indexes")
    def test_prestop_no_indexes(self, mock_get_indexes):
        """Test prestop_handler with no indexes."""
        mock_get_indexes.return_value = []

        result = prestop_handler(base_dir=str(self.base_path))

        self.assertEqual(result, 0)
        # Verify no snapshot directory was created (or it's empty)
        snapshots_dir = self.base_path / "systemsnapshots"
        if snapshots_dir.exists():
            self.assertEqual(len(list(snapshots_dir.iterdir())), 0)

    @patch.dict(os.environ, {"POD_UID": "test-uid-123", "POD_NAME": "test-pod"})
    @patch("manager.get_indexes")
    @patch("manager.persist_index")
    def test_prestop_with_indexes(self, mock_persist, mock_get_indexes):
        """Test prestop_handler persists indexes directly to snapshot directory."""
        mock_get_indexes.return_value = ["index1", "index2"]
        mock_persist.return_value = True

        result = prestop_handler(base_dir=str(self.base_path))

        self.assertEqual(result, 0)
        self.assertEqual(mock_persist.call_count, 2)

        # Verify persist_index was called with snapshot directory paths
        snapshots_dir = self.base_path / "systemsnapshots"
        snapshots = list(snapshots_dir.iterdir())
        self.assertEqual(len(snapshots), 1)
        snapshot = snapshots[0]

        # Check that persist_index was called with snapshot subdirectory paths
        calls = mock_persist.call_args_list
        for call in calls:
            path_arg = call[0][1]  # Second argument is the path
            self.assertIn(str(snapshot), path_arg)

        # Verify metadata
        metadata_file = snapshot / "metadata.json"
        self.assertTrue(metadata_file.exists())

        with open(metadata_file) as f:
            metadata = json.load(f)

        self.assertEqual(metadata["index_names"], ["index1", "index2"])
        self.assertEqual(metadata["pod_name"], "test-pod")
        self.assertEqual(metadata["version"], 1)

        # Verify LATEST link
        latest_link = self.base_path / "LATEST"
        self.assertTrue(latest_link.exists())
        self.assertTrue(latest_link.is_symlink())

    @patch.dict(os.environ, {"POD_UID": "test-uid"})
    @patch("manager.get_indexes")
    @patch("manager.persist_index")
    def test_prestop_cleanup_old_snapshots(self, mock_persist, mock_get_indexes):
        """Test prestop_handler cleans up old snapshots."""
        mock_get_indexes.return_value = ["index1"]
        mock_persist.return_value = True

        # Create 5 old snapshots
        snapshots_dir = self.base_path / "systemsnapshots"
        snapshots_dir.mkdir()

        for i in range(5):
            old_snapshot = snapshots_dir / f"2024-11-{i + 1:02d}T10-00-00_pod-old"
            old_snapshot.mkdir()
            (old_snapshot / "metadata.json").write_text("{}")

        # Run prestop which will create 1 new snapshot (total 6)
        result = prestop_handler(base_dir=str(self.base_path), keep_snapshots=5)

        self.assertEqual(result, 0)

        # After cleanup with keep_snapshots=5, should only have 5 (the newest ones)
        all_snapshots = sorted(
            list(snapshots_dir.iterdir()), key=lambda x: x.name, reverse=True
        )
        self.assertEqual(len(all_snapshots), 5)


class TestLifecycleIntegration(unittest.TestCase):
    """Integration tests for full lifecycle workflows."""

    def setUp(self):
        """Create temporary directory for tests."""
        self.test_dir = tempfile.mkdtemp()
        self.base_path = Path(self.test_dir)

    def tearDown(self):
        """Clean up temporary directory."""
        shutil.rmtree(self.test_dir, ignore_errors=True)

    @patch.dict(os.environ, {"POD_UID": "integration-test", "POD_NAME": "test-pod"})
    @patch("manager.get_indexes")
    @patch("manager.persist_index")
    @patch("manager.wait_for_service")
    @patch("manager.load_index")
    def test_full_lifecycle_workflow(
        self, mock_load, mock_wait, mock_persist, mock_get_indexes
    ):
        """Test complete PreStop -> PostStart workflow."""
        # Setup mocks
        mock_get_indexes.return_value = ["index_alpha", "index_beta"]
        mock_persist.return_value = True
        mock_wait.return_value = True
        mock_load.return_value = True

        # Create mock persisted indexes for PreStop
        for idx in ["index_alpha", "index_beta"]:
            idx_dir = self.base_path / idx
            idx_dir.mkdir()
            (idx_dir / "docstore.json").write_text("{}")
            (idx_dir / "index.faiss").write_text("binary_data")

        # Run PreStop
        prestop_result = prestop_handler(base_dir=str(self.base_path))
        self.assertEqual(prestop_result, 0)

        # Verify snapshot was created
        snapshots_dir = self.base_path / "systemsnapshots"
        self.assertTrue(snapshots_dir.exists())
        snapshots = list(snapshots_dir.iterdir())
        self.assertEqual(len(snapshots), 1)

        # Verify LATEST link exists
        latest_link = self.base_path / "LATEST"
        self.assertTrue(latest_link.exists())

        # Run PostStart (simulate pod restart)
        poststart_result = poststart_handler(base_dir=str(self.base_path))
        self.assertEqual(poststart_result, 0)

        # Verify load_index was called for each index
        self.assertEqual(mock_load.call_count, 2)


if __name__ == "__main__":
    unittest.main()
