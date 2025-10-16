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
Tests for AutoIndexerK8sClient.
"""

import os
from unittest.mock import patch

import pytest
from kubernetes.client.rest import ApiException
from kubernetes.config.config_exception import ConfigException

from autoindexer.k8s.k8s_client import AutoIndexerK8sClient


@pytest.fixture(autouse=True)
def mock_k8s_config():
    """Mock the kubernetes config module."""
    with patch("autoindexer.k8s.k8s_client.config") as mock_config:
        # Make ConfigException available on the mock
        mock_config.ConfigException = ConfigException
        yield mock_config


@pytest.fixture
def mock_custom_api():
    """Mock CustomObjectsApi."""
    with patch('autoindexer.k8s.k8s_client.client.CustomObjectsApi') as mock_api:
        yield mock_api.return_value


@pytest.fixture
def mock_core_api():
    """Mock CoreV1Api."""
    with patch('autoindexer.k8s.k8s_client.client.CoreV1Api') as mock_api:
        yield mock_api.return_value


@pytest.fixture
def sample_autoindexer_crd():
    """Sample AutoIndexer CRD object."""
    return {
        "apiVersion": "kaito.sh/v1alpha1",
        "kind": "AutoIndexer",
        "metadata": {
            "name": "test-autoindexer",
            "namespace": "default"
        },
        "spec": {
            "indexName": "test-index",
            "ragEngine": "test-rag-engine",
            "dataSource": {
                "type": "Git",
                "git": {
                    "repository": "https://github.com/example/repo.git",
                    "branch": "main",
                    "paths": ["/docs"],
                    "excludePaths": ["/docs/private"]
                }
            }
        },
        "status": {
            "conditions": [],
            "indexingPhase": "Pending",
            "numOfDocumentInIndex": 0,
            "successfulIndexingCount": 0,
            "errorIndexingCount": 0
        }
    }


class TestAutoIndexerK8sClient:
    """Test cases for AutoIndexerK8sClient."""

    def test_init_in_cluster_config(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test initialization with in-cluster config."""
        # Mock successful in-cluster config loading
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock environment variables
        with patch.dict(os.environ, {
            'NAMESPACE': 'test-namespace',
            'AUTOINDEXER_NAME': 'test-autoindexer'
        }):
            client = AutoIndexerK8sClient()
            
            assert client.api_group == "kaito.sh"
            assert client.api_version == "v1alpha1"
            assert client.plural == "autoindexers"
            assert client.kind == "AutoIndexer"
            assert client.namespace == "test-namespace"
            assert client.autoindexer_name == "test-autoindexer"
            
            mock_k8s_config.load_incluster_config.assert_called_once()

    def test_init_kubeconfig_fallback(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test initialization with kubeconfig fallback when incluster config fails."""
        # Mock incluster config to fail, kubeconfig to succeed
        mock_k8s_config.load_incluster_config.side_effect = ConfigException("Not in cluster")
        mock_k8s_config.load_kube_config.return_value = None  # Success
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'test-ns'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-ai'):
            client = AutoIndexerK8sClient()
            
        # Verify both config methods were called
        mock_k8s_config.load_incluster_config.assert_called_once()
        mock_k8s_config.load_kube_config.assert_called_once()
        
        assert client.namespace == "test-ns"
        assert client.autoindexer_name == "test-ai"

    def test_init_config_failure(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test initialization failure when both config methods fail."""
        # Mock both config methods to fail
        mock_k8s_config.load_incluster_config.side_effect = ConfigException("Not in cluster")
        mock_k8s_config.load_kube_config.side_effect = ConfigException("No kubeconfig")
        
        with pytest.raises(ConfigException):
            AutoIndexerK8sClient()

    def test_get_current_namespace_from_env(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test getting namespace from environment variable."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'custom-namespace'):
            client = AutoIndexerK8sClient()
            assert client.namespace == "custom-namespace"

    def test_get_current_namespace_from_service_account(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test getting namespace from service account token."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'service-account-namespace'):
            client = AutoIndexerK8sClient()
            assert client.namespace == "service-account-namespace"

    def test_get_current_namespace_default_fallback(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test fallback to default namespace."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'):
            client = AutoIndexerK8sClient()
            assert client.namespace == "default"

    def test_get_autoindexer_success(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test successful retrieval of AutoIndexer CRD."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            result = client.get_autoindexer()
            
            assert result == sample_autoindexer_crd
            mock_custom_api.get_namespaced_custom_object.assert_called_once_with(
                group="kaito.sh",
                version="v1alpha1",
                namespace="default",
                plural="autoindexers",
                name="test-autoindexer"
            )

    def test_get_autoindexer_not_found(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test AutoIndexer CRD not found."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock 404 error
        api_error = ApiException(status=404)
        mock_custom_api.get_namespaced_custom_object.side_effect = api_error
        
        with patch.dict(os.environ, {
            'NAMESPACE': 'default',
            'AUTOINDEXER_NAME': 'test-autoindexer'
        }):
            client = AutoIndexerK8sClient()
            result = client.get_autoindexer()
            
            assert result is None

    def test_get_autoindexer_api_error(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test AutoIndexer CRD API error."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock non-404 error
        api_error = ApiException(status=500)
        mock_custom_api.get_namespaced_custom_object.side_effect = api_error
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            with pytest.raises(ApiException):
                client.get_autoindexer()

    def test_get_autoindexer_no_name(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test getting AutoIndexer with no name specified."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', ''):
            client = AutoIndexerK8sClient()
            result = client.get_autoindexer()
            
            assert result is None

    def test_update_autoindexer_status_success(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test successful status update."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock successful get and patch
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        mock_custom_api.patch_namespaced_custom_object_status.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            status_update = {"indexingPhase": "Running"}
            result = client.update_autoindexer_status(status_update)
            
            assert result is True
            mock_custom_api.patch_namespaced_custom_object_status.assert_called_once()

    def test_update_autoindexer_status_not_found(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test status update when AutoIndexer not found."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock 404 error on get
        api_error = ApiException(status=404)
        mock_custom_api.get_namespaced_custom_object.side_effect = api_error
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            status_update = {"indexingPhase": "Running"}
            result = client.update_autoindexer_status(status_update)
            
            assert result is False

    def test_add_status_condition_new(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test adding a new status condition."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock successful get and patch
        crd_copy = sample_autoindexer_crd.copy()
        crd_copy["status"]["conditions"] = []
        mock_custom_api.get_namespaced_custom_object.return_value = crd_copy
        mock_custom_api.patch_namespaced_custom_object_status.return_value = crd_copy
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            result = client.add_status_condition(
                "AutoIndexerIndexing", 
                "True", 
                "IndexingStarted", 
                "Document indexing process has started"
            )
            
            assert result is True
            mock_custom_api.patch_namespaced_custom_object_status.assert_called_once()

    def test_add_status_condition_update_existing(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test updating an existing status condition."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock CRD with existing condition
        crd_copy = sample_autoindexer_crd.copy()
        crd_copy["status"]["conditions"] = [{
            "type": "AutoIndexerIndexing",
            "status": "False",
            "reason": "NotStarted",
            "message": "Not started yet",
            "lastTransitionTime": "2023-01-01T00:00:00Z"
        }]
        mock_custom_api.get_namespaced_custom_object.return_value = crd_copy
        mock_custom_api.patch_namespaced_custom_object_status.return_value = crd_copy
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            result = client.add_status_condition(
                "AutoIndexerIndexing", 
                "True", 
                "IndexingStarted", 
                "Document indexing process has started"
            )
            
            assert result is True

    def test_update_indexing_progress(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test updating indexing progress."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        mock_custom_api.patch_namespaced_custom_object_status.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            result = client.update_indexing_progress(100)
            
            assert result is True
            mock_custom_api.patch_namespaced_custom_object_status.assert_called_once()

    def test_update_indexing_phase(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test updating indexing phase."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        mock_custom_api.patch_namespaced_custom_object_status.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            result = client.update_indexing_phase("Running")
            
            assert result is True

    def test_update_indexing_completion_success(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test updating indexing completion for success."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Mock get calls for counter increment
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        mock_custom_api.patch_namespaced_custom_object_status.return_value = sample_autoindexer_crd
        
        with patch.dict(os.environ, {
            'NAMESPACE': 'default',
            'AUTOINDEXER_NAME': 'test-autoindexer'
        }):
            client = AutoIndexerK8sClient()
            
            result = client.update_indexing_completion(
                success=True,
                duration_seconds=120,
                document_count=50,
                commit_hash="abc123"
            )
            
            assert result is True

    def test_update_indexing_completion_failure(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test updating indexing completion for failure."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        mock_custom_api.patch_namespaced_custom_object_status.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            
            result = client.update_indexing_completion(
                success=False,
                duration_seconds=60,
                document_count=0
            )
            
            assert result is True

    def test_get_autoindexer_config(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test getting AutoIndexer configuration from spec."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            config = client.get_autoindexer_config()
            
            assert config == sample_autoindexer_crd["spec"]
            assert config["indexName"] == "test-index"
            assert config["ragEngine"] == "test-rag-engine"

    def test_get_autoindexer_config_not_found(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test getting AutoIndexer configuration when CRD not found."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        api_error = ApiException(status=404)
        mock_custom_api.get_namespaced_custom_object.side_effect = api_error
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            config = client.get_autoindexer_config()
            
            assert config is None

    def test_increment_counter_success(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test incrementing counter with existing value."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        # Set up CRD with existing counter value
        crd_copy = sample_autoindexer_crd.copy()
        crd_copy["status"]["successfulIndexingCount"] = 5
        mock_custom_api.get_namespaced_custom_object.return_value = crd_copy
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            result = client._increment_counter("successfulIndexingCount")
            
            assert result == 6

    def test_increment_counter_no_existing_value(self, mock_k8s_config, mock_custom_api, mock_core_api, sample_autoindexer_crd):
        """Test incrementing counter with no existing value."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        mock_custom_api.get_namespaced_custom_object.return_value = sample_autoindexer_crd.copy()
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            result = client._increment_counter("newCounter")
            
            assert result == 1

    def test_increment_counter_error(self, mock_k8s_config, mock_custom_api, mock_core_api):
        """Test incrementing counter with error getting current value."""
        mock_k8s_config.load_incluster_config.return_value = None
        mock_k8s_config.ConfigException = Exception
        
        api_error = ApiException(status=500)
        mock_custom_api.get_namespaced_custom_object.side_effect = api_error
        
        with patch('autoindexer.k8s.k8s_client.NAMESPACE', 'default'), \
             patch('autoindexer.k8s.k8s_client.AUTOINDEXER_NAME', 'test-autoindexer'):
            client = AutoIndexerK8sClient()
            result = client._increment_counter("errorCounter")
            
            assert result == 1