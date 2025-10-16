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


import json
import os
from unittest.mock import Mock, patch

import pytest
from requests.exceptions import RequestException

from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.main import AutoIndexerService, main


class TestAutoIndexerService:
    """Integration tests for AutoIndexer service."""

    @pytest.fixture
    def valid_env_vars(self):
        """Fixture providing valid environment variables."""
        return {
            "ACCESS_SECRET": "test-secret",
            "AUTOINDEXER_NAME": "test-autoindexer",
            "NAMESPACE": "test-namespace"
        }

    @pytest.fixture
    def mock_k8s_client(self):
        """Fixture providing a mock Kubernetes client."""
        with patch('autoindexer.main.AutoIndexerK8sClient') as mock_class:
            mock_client = Mock()
            mock_client.namespace = "test-namespace"
            mock_client.get_autoindexer_config.return_value = {
                "indexName": "test-index",
                "ragEngine": "test-rag-engine",
                "dataSource": {
                    "type": "Static",
                    "static": {
                        "urls": ["https://example.com/doc.txt"]
                    }
                }
            }
            mock_client.add_status_condition.return_value = None
            mock_client.update_indexing_progress.return_value = None
            mock_client.update_indexing_phase.return_value = None
            mock_client.update_indexing_completion.return_value = None
            mock_class.return_value = mock_client
            yield mock_client

    @pytest.fixture
    def mock_rag_client(self):
        """Fixture providing a mock RAG client."""
        with patch('autoindexer.main.KAITORAGClient') as mock_class:
            mock_client = Mock()
            mock_client.list_indexes.return_value = {"indexes": [{"name": "test-index"}]}
            mock_client.list_documents.return_value = {"total": 5, "documents": []}
            mock_class.return_value = mock_client
            yield mock_class

    @pytest.fixture
    def mock_static_handler(self):
        """Fixture providing a mock static data source handler."""
        with patch('autoindexer.main.StaticDataSourceHandler') as mock_class:
            mock_handler = Mock()
            mock_handler.update_index.return_value = []  # No errors
            mock_class.return_value = mock_handler
            yield mock_class

    @pytest.fixture
    def mock_git_handler(self):
        """Fixture providing a mock git data source handler."""
        with patch('autoindexer.main.GitDataSourceHandler') as mock_class:
            mock_handler = Mock()
            mock_handler.update_index.return_value = []  # No errors
            mock_class.return_value = mock_handler
            yield mock_class

    def test_init_success_with_static_data_source(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test successful initialization with static data source."""
        with patch.dict(os.environ, valid_env_vars):
            try:
                service = AutoIndexerService()

                assert service.access_secret == "test-secret"
                assert service.autoindexer_name == "test-autoindexer"
                assert service.namespace == "test-namespace"
                assert service.index_name == "test-index"
                assert service.datasource_type == "Static"
                assert service.ragengine_endpoint == "http://test-rag-engine.test-namespace.svc.cluster.local:80"

                # Verify K8s client was initialized
                mock_k8s_client.get_autoindexer_config.assert_called_once()

                # Verify RAG client was initialized
                mock_rag_client.assert_called_once_with("http://test-rag-engine.test-namespace.svc.cluster.local:80")
                assert hasattr(service, 'rag_client')
                assert service.rag_client == mock_rag_client.return_value
                
                # Verify static handler was created with expected config
                expected_config = {
                    "autoindexer_name": "test-autoindexer",
                    "urls": ["https://example.com/doc.txt"]
                }
                mock_static_handler.assert_called_once_with(
                    index_name="test-index",
                    config=expected_config, 
                    rag_client=mock_rag_client.return_value,
                    autoindexer_client=mock_k8s_client,
                    credentials="test-secret"
                )
                assert hasattr(service, 'data_source_handler')
                assert service.data_source_handler == mock_static_handler.return_value
                
            except Exception as e:
                print(f"Exception during service initialization: {e}")
                import traceback
                traceback.print_exc()
                raise

    def test_init_success_with_git_data_source(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_git_handler):
        """Test successful initialization with git data source."""
        # Update mock to return git config
        mock_k8s_client.get_autoindexer_config.return_value = {
            "indexName": "test-index",
            "ragEngine": "test-rag-engine", 
            "dataSource": {
                "type": "Git",
                "git": {
                    "repository": "https://github.com/test/repo.git",
                    "branch": "main",
                    "paths": ["/docs"]
                }
            }
        }
        
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            assert service.datasource_type == "Git"
            assert service.datasource_config["repository"] == "https://github.com/test/repo.git"
            
            # Verify git handler was created
            mock_git_handler.assert_called_once()

    def test_init_missing_required_env_var(self):
        """Test initialization failure with missing required environment variables."""
        incomplete_env = {"ACCESS_SECRET": "test-secret"}  # Missing other required vars
        
        with patch.dict(os.environ, incomplete_env, clear=True), \
             pytest.raises(ValueError, match="RAG engine endpoint must be configured"):
            AutoIndexerService()

    def test_init_k8s_client_failure(self, valid_env_vars):
        """Test handling of Kubernetes client initialization failure."""
        with patch('autoindexer.main.AutoIndexerK8sClient') as mock_class:
            mock_class.side_effect = Exception("K8s connection failed")
            
            with patch.dict(os.environ, valid_env_vars), \
                 pytest.raises(Exception, match="K8s connection failed"):
                AutoIndexerService()

    def test_init_invalid_crd_config_missing_index_name(self, valid_env_vars, mock_rag_client, mock_static_handler):
        """Test initialization failure with invalid CRD config missing index name."""
        with patch('autoindexer.main.AutoIndexerK8sClient') as mock_class:
            mock_client = Mock()
            mock_client.get_autoindexer_config.return_value = {
                "ragEngine": "test-rag-engine",
                # Missing indexName
                "dataSource": {
                    "type": "Static",
                    "static": {"urls": ["https://example.com/doc.txt"]}
                }
            }
            mock_class.return_value = mock_client
            
            with patch.dict(os.environ, valid_env_vars), \
                 pytest.raises(ValueError, match="indexName must be specified"):
                AutoIndexerService()

    def test_init_unsupported_data_source_type(self, valid_env_vars, mock_k8s_client, mock_rag_client):
        """Test initialization failure with unsupported data source type."""
        mock_k8s_client.get_autoindexer_config.return_value = {
            "indexName": "test-index",
            "ragEngine": "test-rag-engine",
            "dataSource": {
                "type": "UnsupportedType",
                "unsupported": {}
            }
        }
        
        with patch.dict(os.environ, valid_env_vars), \
             pytest.raises(ValueError, match="Unsupported or missing data source configuration in CRD"):
            AutoIndexerService()

    def test_run_success(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test successful indexing run."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock successful indexing on the actual instances
            service.data_source_handler.update_index.return_value = []  # No errors
            service.rag_client.list_documents.return_value = {"total": 5}
            
            with patch('time.time', side_effect=[1000.0, 1030.0]):  # 30 second duration
                result = service.run()
            
            assert result is True
            
            # Verify status updates were called
            service.k8s_client.update_indexing_phase.assert_called()
            service.k8s_client.add_status_condition.assert_called()
            service.k8s_client.update_indexing_completion.assert_called_with(True, 30, 5, None)
            
            # Verify indexing was performed
            service.data_source_handler.update_index.assert_called_once_with()

    def test_run_with_indexing_errors(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test run with indexing errors."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock indexing with errors
            service.data_source_handler.update_index.return_value = ["Error fetching document", "Network timeout"]
            service.rag_client.list_documents.return_value = {"total": 0}
            
            with patch('time.time', side_effect=[1000.0] + [1025.0] * 10):  # 25 second duration, multiple calls
                result = service.run()
            
            assert result is False
            
            # Verify failure status was set
            service.k8s_client.update_indexing_completion.assert_called_with(False, 25, 0, None)
            
            # Verify error condition was set
            status_calls = mock_k8s_client.add_status_condition.call_args_list
            error_calls = [call for call in status_calls if call[0][0] == "AutoIndexerError"]
            assert len(error_calls) > 0

    def test_run_with_exception(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test run with unexpected exception."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock exception during indexing
            service.data_source_handler.update_index.side_effect = Exception("Unexpected error")
            service.rag_client.list_documents.return_value = {"total": 0}
            
            with patch('time.time', side_effect=[1000.0] + [1020.0] * 10):  # 20 second duration, multiple calls
                result = service.run()
            
            assert result is False
            
            # Verify failure status was set
            service.k8s_client.update_indexing_completion.assert_called_with(False, 20, 0, None)

    def test_run_dry_run_mode(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test dry run mode."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService(dry_run=True)
            
            assert service.dry_run is True
            
            # Dry run should still execute the same logic
            result = service.run()
            assert result is True

    def test_update_index_data_source_error(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test _update_index with DataSourceError."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock DataSourceError
            service.data_source_handler.update_index.side_effect = DataSourceError("Data source failed")
            
            errors = service._update_index()
            
            assert len(errors) == 1
            assert "Data source error: Data source failed" in errors[0]

    def test_update_index_unexpected_error(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test _update_index with unexpected error."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock unexpected error
            service.data_source_handler.update_index.side_effect = RuntimeError("Unexpected runtime error")
            
            errors = service._update_index()
            
            assert len(errors) == 1
            assert "Unexpected error during indexing: Unexpected runtime error" in errors[0]

    def test_ensure_index_exists_index_present(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test _ensure_index_exists when index already exists."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            service.rag_client.list_indexes.return_value = {
                "indexes": [{"name": "test-index"}, {"name": "other-index"}]
            }
            
            # Should not raise any exception
            service._ensure_index_exists()
            
            service.rag_client.list_indexes.assert_called_once()

    def test_ensure_index_exists_index_missing(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test _ensure_index_exists when index doesn't exist."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            service.rag_client.list_indexes.return_value = {
                "indexes": [{"name": "other-index"}]
            }
            
            # Should not raise any exception
            service._ensure_index_exists()
            
            service.rag_client.list_indexes.assert_called_once()

    def test_ensure_index_exists_rag_error(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test _ensure_index_exists when RAG client raises error."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            mock_rag_client.list_indexes.side_effect = RequestException("RAG engine unavailable")
            
            # Should not raise any exception, just log warning
            service._ensure_index_exists()

    def test_status_updates_without_k8s_client(self, valid_env_vars, mock_rag_client, mock_static_handler):
        """Test status update methods when K8s client is not available."""
        with patch('autoindexer.main.AutoIndexerK8sClient') as mock_class:
            mock_class.side_effect = Exception("K8s unavailable")
            
            with patch.dict(os.environ, valid_env_vars), \
                 pytest.raises(Exception):
                AutoIndexerService()

    def test_status_updates_k8s_client_errors(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test status update methods when K8s client operations fail."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            # Mock K8s client methods to raise exceptions
            mock_k8s_client.add_status_condition.side_effect = Exception("K8s API error")
            mock_k8s_client.update_indexing_progress.side_effect = Exception("K8s API error")
            mock_k8s_client.update_indexing_phase.side_effect = Exception("K8s API error")
            mock_k8s_client.update_indexing_completion.side_effect = Exception("K8s API error")
            
            # These should not raise exceptions, just log warnings
            service._update_status_condition("Test", "True", "Reason", "Message")
            service._update_indexing_progress(10, 5)
            service._update_indexing_phase("Running")
            service._update_indexing_completion(True, 30, 5)
            
            # Verify methods were called despite errors
            mock_k8s_client.add_status_condition.assert_called()
            mock_k8s_client.update_indexing_progress.assert_called()
            mock_k8s_client.update_indexing_phase.assert_called()
            mock_k8s_client.update_indexing_completion.assert_called()

    def test_apply_crd_config_git_source(self, valid_env_vars, mock_rag_client, mock_git_handler):
        """Test applying CRD configuration for Git data source."""
        with patch('autoindexer.main.AutoIndexerK8sClient') as mock_class:
            mock_client = Mock()
            mock_client.namespace = "test-namespace"
            mock_client.get_autoindexer_config.return_value = {
                "indexName": "git-index",
                "ragEngine": "rag-engine",
                "dataSource": {
                    "type": "Git",
                    "git": {
                        "repository": "https://github.com/test/repo.git",
                        "branch": "develop",
                        "commit": "abc123",
                        "paths": ["/docs", "/examples"],
                        "excludePaths": ["/docs/internal"]
                    }
                }
            }
            mock_class.return_value = mock_client
            
            with patch.dict(os.environ, valid_env_vars):
                service = AutoIndexerService()
                
                assert service.index_name == "git-index"
                assert service.datasource_type == "Git"
                assert service.datasource_config["repository"] == "https://github.com/test/repo.git"
                assert service.datasource_config["branch"] == "develop"
                assert service.datasource_config["commit"] == "abc123"
                assert service.datasource_config["paths"] == ["/docs", "/examples"]
                assert service.datasource_config["excludePaths"] == ["/docs/internal"]

    def test_optional_env_json_valid(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test parsing of optional JSON environment variables."""
        json_config = {"key": "value", "number": 42}
        env_vars = {**valid_env_vars, "TEST_JSON_CONFIG": json.dumps(json_config)}
        
        with patch.dict(os.environ, env_vars):
            service = AutoIndexerService()
            
            result = service._get_optional_env_json("TEST_JSON_CONFIG")
            assert result == json_config

    def test_optional_env_json_invalid(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test parsing of invalid JSON environment variables."""
        env_vars = {**valid_env_vars, "INVALID_JSON": "not valid json {"}
        
        with patch.dict(os.environ, env_vars):
            service = AutoIndexerService()
            
            result = service._get_optional_env_json("INVALID_JSON")
            assert result is None

    def test_optional_env_json_missing(self, valid_env_vars, mock_k8s_client, mock_rag_client, mock_static_handler):
        """Test parsing of missing JSON environment variables."""
        with patch.dict(os.environ, valid_env_vars):
            service = AutoIndexerService()
            
            result = service._get_optional_env_json("MISSING_JSON")
            assert result is None


class TestMainFunction:
    """Tests for the main() function and CLI interface."""

    def test_main_success_default_args(self):
        """Test main function with default arguments."""
        test_args = ["main.py"]  # Default mode=index, no dry-run
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('sys.exit') as mock_exit:
            mock_service = Mock()
            mock_service.run.return_value = True
            mock_service_class.return_value = mock_service
            
            main()
            
            mock_service_class.assert_called_once_with(dry_run=False)
            mock_service.run.assert_called_once()
            mock_exit.assert_called_once_with(0)

    def test_main_success_with_dry_run(self):
        """Test main function with dry-run flag."""
        test_args = ["main.py", "--dry-run"]
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('sys.exit') as mock_exit:
            mock_service = Mock()
            mock_service.run.return_value = True
            mock_service_class.return_value = mock_service
            
            main()
            
            mock_service_class.assert_called_once_with(dry_run=True)
            mock_exit.assert_called_once_with(0)

    def test_main_failure_service_returns_false(self):
        """Test main function when service returns False."""
        test_args = ["main.py"]
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('sys.exit') as mock_exit:
            mock_service = Mock()
            mock_service.run.return_value = False
            mock_service_class.return_value = mock_service
            
            main()
            
            mock_exit.assert_called_once_with(1)

    def test_main_failure_service_initialization_error(self):
        """Test main function when service initialization fails."""
        test_args = ["main.py"]
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('sys.exit') as mock_exit:
            mock_service_class.side_effect = Exception("Initialization failed")
            
            main()
            
            mock_exit.assert_called_once_with(1)

    def test_main_with_log_level(self):
        """Test main function with custom log level."""
        test_args = ["main.py", "--log-level", "DEBUG"]
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('logging.getLogger') as mock_get_logger, \
             patch('sys.exit'):
            mock_service = Mock()
            mock_service.run.return_value = True
            mock_service_class.return_value = mock_service
            
            mock_logger = Mock()
            mock_get_logger.return_value = mock_logger
            
            main()
            
            # Verify log level was set
            mock_logger.setLevel.assert_called_once()

    def test_main_index_mode(self):
        """Test main function with explicit index mode."""
        test_args = ["main.py", "--mode", "index"]
        
        with patch('sys.argv', test_args), \
             patch('autoindexer.main.AutoIndexerService') as mock_service_class, \
             patch('sys.exit') as mock_exit:
            mock_service = Mock()
            mock_service.run.return_value = True
            mock_service_class.return_value = mock_service
            
            main()
            
            mock_service_class.assert_called_once_with(dry_run=False)
            mock_exit.assert_called_once_with(0)