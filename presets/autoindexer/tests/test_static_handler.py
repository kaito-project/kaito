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
from unittest.mock import Mock, mock_open, patch

import pytest
from requests.exceptions import RequestException

from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.data_source_handler.static_handler import StaticDataSourceHandler
from autoindexer.rag.rag_client import KAITORAGClient


class TestStaticDataSourceHandler:
    """Test class for StaticDataSourceHandler."""

    @pytest.fixture
    def valid_config(self):
        """Fixture providing a valid configuration."""
        return {
            "autoindexer_name": "test-autoindexer",
            "static": {
                "urls": ["https://example.com/document.md"],
                "timeout": 30,
                "max_file_size": 5 * 1024 * 1024  # 5MB
            }
        }

    @pytest.fixture
    def credentials(self):
        """Fixture providing test credentials."""
        return {
            "http_auth": {
                "username": "testuser",
                "password": "testpass"
            },
            "headers": {
                "X-Custom-Header": "test-value"
            }
        }

    @pytest.fixture
    def mock_rag_client(self):
        """Fixture providing a mock RAG client."""
        client = Mock(spec=KAITORAGClient)
        client.index_documents.return_value = {"success": True, "indexed": 1}
        return client

    def test_init_success(self, valid_config):
        """Test successful initialization."""
        handler = StaticDataSourceHandler(valid_config)
        
        assert handler.config == valid_config
        assert handler.credentials == {}
        assert handler.autoindexer_name == "test-autoindexer"
        assert handler.static_config == valid_config["static"]

    def test_init_with_credentials(self, valid_config, credentials):
        """Test initialization with credentials."""
        handler = StaticDataSourceHandler(valid_config, credentials)
        
        assert handler.credentials == credentials

    def test_init_missing_autoindexer_name(self):
        """Test initialization failure with missing autoindexer_name."""
        config = {
            "static": {
                "urls": ["https://example.com/document.md"]
            }
        }
        
        with pytest.raises(DataSourceError, match="missing 'autoindexer_name' value"):
            StaticDataSourceHandler(config)

    def test_init_missing_static_section(self):
        """Test initialization failure with missing static section."""
        config = {
            "autoindexer_name": "test-autoindexer"
        }
        
        with pytest.raises(DataSourceError, match="missing 'static' section"):
            StaticDataSourceHandler(config)

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_single_url_success(self, mock_get, valid_config, mock_rag_client):
        """Test successful index update with a single URL."""
        # Setup mock response
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Test content from URL']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config)
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert errors == []
        mock_rag_client.index_documents.assert_called_once()
        
        call_args = mock_rag_client.index_documents.call_args
        assert call_args[1]["index_name"] == "test-index"
        documents = call_args[1]["documents"]
        assert len(documents) == 1
        assert documents[0]["text"] == "Test content from URL"
        assert documents[0]["metadata"]["source_type"] == "url"
        assert documents[0]["metadata"]["source_url"] == "https://example.com/document.md"
        assert documents[0]["metadata"]["autoindexer"] == "test-autoindexer"

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_multiple_urls(self, mock_get, mock_rag_client):
        """Test index update with multiple URLs."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "static": {
                "urls": [
                    "https://example.com/doc1.md",
                    "https://example.com/doc2.txt"
                ]
            }
        }
        
        # Setup mock responses
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.side_effect = [
            [b'Content from doc1'],
            [b'Content from doc2']
        ]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(config)
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert errors == []
        assert mock_rag_client.index_documents.call_count == 1
        
        call_args = mock_rag_client.index_documents.call_args
        documents = call_args[1]["documents"]
        assert len(documents) == 2

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_batch_processing(self, mock_get, mock_rag_client):
        """Test batch processing when document count exceeds batch size."""
        # Create config with 12 URLs to trigger batch processing (batch size is 10)
        urls = [f"https://example.com/doc{i}.md" for i in range(12)]
        config = {
            "autoindexer_name": "test-autoindexer",
            "static": {
                "urls": urls
            }
        }
        
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Test content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(config)
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert errors == []
        # Should be called twice: once for first 10, once for remaining 2
        assert mock_rag_client.index_documents.call_count == 2

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_http_error(self, mock_get, valid_config, mock_rag_client):
        """Test handling of HTTP errors during index update."""
        mock_get.return_value.__enter__.side_effect = RequestException("Network error")
        
        handler = StaticDataSourceHandler(valid_config)
        
        # The method catches exceptions and returns them as errors instead of raising
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert len(errors) == 1
        assert "Failed to fetch content" in errors[0]

    def test_update_index_no_urls(self, mock_rag_client):
        """Test handling when no URLs are configured."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "static": {}
        }
        
        handler = StaticDataSourceHandler(config)
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert len(errors) == 1
        assert "No documents fetched" in errors[0]

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_documents_success(self, mock_get, valid_config):
        """Test successful document fetching."""
        # Update config to use 'endpoints' for fetch_documents method
        config = {
            "autoindexer_name": "test-autoindexer",
            "static": {
                "endpoints": ["https://example.com/document.md"]
            }
        }
        
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Test document content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(config)
        documents = handler.fetch_documents()
        
        assert len(documents) == 1
        assert documents[0]["text"] == "Test document content"
        assert documents[0]["metadata"]["source_type"] == "url"

    def test_fetch_documents_no_endpoints(self, valid_config):
        """Test fetch_documents with no endpoints configured."""
        handler = StaticDataSourceHandler(valid_config)
        documents = handler.fetch_documents()
        
        assert documents == []

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_from_url_success(self, mock_get, valid_config):
        """Test successful content fetching from URL."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain; charset=utf-8'}
        mock_response.iter_content.return_value = [b'Test content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config)
        content = handler._fetch_content_from_url("https://example.com/test.txt")
        
        assert content == "Test content"

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_invalid_url(self, mock_get, valid_config):
        """Test handling of invalid URLs."""
        handler = StaticDataSourceHandler(valid_config)
        
        with pytest.raises(DataSourceError, match="Invalid URL format"):
            handler._fetch_content_from_url("not-a-valid-url")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_file_too_large_header(self, mock_get, valid_config):
        """Test handling of files that are too large based on Content-Length header."""
        mock_response = Mock()
        mock_response.headers = {'content-length': str(20 * 1024 * 1024)}  # 20MB
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config)
        
        with pytest.raises(DataSourceError, match="File too large"):
            handler._fetch_content_from_url("https://example.com/large-file.txt")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_file_too_large_streaming(self, mock_get, valid_config):
        """Test handling of files that are too large during streaming."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        # Create a large chunk that exceeds the limit
        large_chunk = b'x' * (6 * 1024 * 1024)  # 6MB chunk
        mock_response.iter_content.return_value = [large_chunk]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config)
        
        with pytest.raises(DataSourceError, match="File too large"):
            handler._fetch_content_from_url("https://example.com/large-file.txt")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_with_authentication(self, mock_get, valid_config, credentials):
        """Test content fetching with HTTP authentication."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Authenticated content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config, credentials)
        handler._fetch_content_from_url("https://example.com/secure.txt")
        
        # Verify authentication was used
        mock_get.assert_called_once()
        call_kwargs = mock_get.call_args[1]
        assert call_kwargs['auth'] == ('testuser', 'testpass')
        assert 'X-Custom-Header' in call_kwargs['headers']

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_json_processing(self, mock_get, valid_config):
        """Test handling of JSON content with special processing."""
        json_data = {"content": "This is the main content", "other": "ignored"}
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'application/json'}
        mock_response.iter_content.return_value = [json.dumps(json_data).encode('utf-8')]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(valid_config)
        content = handler._fetch_content_from_url("https://example.com/data.json")
        
        assert content == "This is the main content"

    def test_is_file_url_file_extensions(self, valid_config):
        """Test file URL detection based on file extensions."""
        handler = StaticDataSourceHandler(valid_config)
        
        # Test various file extensions
        file_urls = [
            "https://example.com/doc.txt",
            "https://example.com/readme.md",
            "https://example.com/config.json",
            "https://example.com/data.csv",
            "https://example.com/script.py",
            "https://example.com/document.pdf"
        ]
        
        for url in file_urls:
            from urllib.parse import urlparse
            parsed_url = urlparse(url)
            assert handler._is_file_url(url, parsed_url) is True

    def test_is_file_url_github_raw(self, valid_config):
        """Test file URL detection for GitHub raw URLs."""
        handler = StaticDataSourceHandler(valid_config)
        
        url = "https://raw.githubusercontent.com/user/repo/main/README.md"
        from urllib.parse import urlparse
        parsed_url = urlparse(url)
        
        assert handler._is_file_url(url, parsed_url) is True

    def test_is_file_url_non_file(self, valid_config):
        """Test file URL detection for non-file URLs."""
        handler = StaticDataSourceHandler(valid_config)
        
        non_file_urls = [
            "https://example.com/",
            "https://example.com/page",
            "https://example.com/search?q=test"
        ]
        
        for url in non_file_urls:
            from urllib.parse import urlparse
            parsed_url = urlparse(url)
            assert handler._is_file_url(url, parsed_url) is False

    def test_is_pdf_content_content_type(self, valid_config):
        """Test PDF detection based on content type."""
        handler = StaticDataSourceHandler(valid_config)
        
        assert handler._is_pdf_content(b'', 'application/pdf', 'test.pdf') is True
        assert handler._is_pdf_content(b'', 'text/plain', 'test.txt') is False

    def test_is_pdf_content_url_extension(self, valid_config):
        """Test PDF detection based on URL extension."""
        handler = StaticDataSourceHandler(valid_config)
        
        assert handler._is_pdf_content(b'', '', 'document.pdf') is True
        assert handler._is_pdf_content(b'', '', 'document.txt') is False

    def test_is_pdf_content_magic_bytes(self, valid_config):
        """Test PDF detection based on magic bytes."""
        handler = StaticDataSourceHandler(valid_config)
        
        pdf_content = b'%PDF-1.4\n...'
        text_content = b'This is plain text'
        
        assert handler._is_pdf_content(pdf_content, '', 'unknown') is True
        assert handler._is_pdf_content(text_content, '', 'unknown') is False

    def test_extract_pdf_text_pypdf2_success(self, valid_config):
        """Test successful PDF text extraction using PyPDF2."""
        with patch('builtins.__import__') as mock_import:
            # Mock PyPDF2 module
            mock_pypdf2 = Mock()
            mock_page = Mock()
            mock_page.extract_text.return_value = "Page content"
            
            mock_reader = Mock()
            mock_reader.pages = [mock_page]
            
            mock_pypdf2.PdfReader.return_value = mock_reader
            
            def import_side_effect(name, *args, **kwargs):
                if name == 'PyPDF2':
                    return mock_pypdf2
                return __import__(name, *args, **kwargs)
            
            mock_import.side_effect = import_side_effect
            
            handler = StaticDataSourceHandler(valid_config)
            result = handler._extract_pdf_text(b'%PDF-1.4...', 'test.pdf')
            
            assert "Page content" in result
            assert "--- Page 1 ---" in result

    def test_extract_pdf_text_pdfplumber_fallback(self, valid_config):
        """Test PDF text extraction fallback to pdfplumber."""
        with patch('builtins.__import__') as mock_import:
            # Mock pdfplumber module and PyPDF2 import error
            mock_pdfplumber = Mock()
            mock_page = Mock()
            mock_page.extract_text.return_value = "Page content from pdfplumber"
            mock_page.extract_tables.return_value = []
            
            mock_pdf = Mock()
            mock_pdf.pages = [mock_page]
            mock_pdf.__enter__ = Mock(return_value=mock_pdf)
            mock_pdf.__exit__ = Mock(return_value=None)
            
            mock_pdfplumber.open.return_value = mock_pdf
            
            def import_side_effect(name, *args, **kwargs):
                if name == 'PyPDF2':
                    raise ImportError("PyPDF2 not available")
                elif name == 'pdfplumber':
                    return mock_pdfplumber
                return __import__(name, *args, **kwargs)
            
            mock_import.side_effect = import_side_effect
            
            handler = StaticDataSourceHandler(valid_config)
            result = handler._extract_pdf_text(b'%PDF-1.4...', 'test.pdf')
            
            assert "Page content from pdfplumber" in result

    def test_extract_pdf_text_no_libraries(self, valid_config):
        """Test PDF text extraction failure when no libraries are available."""
        with patch('builtins.__import__') as mock_import:
            def import_side_effect(name, *args, **kwargs):
                if name in ['PyPDF2', 'pdfplumber']:
                    raise ImportError(f"{name} not available")
                return __import__(name, *args, **kwargs)
            
            mock_import.side_effect = import_side_effect
            
            handler = StaticDataSourceHandler(valid_config)
            
            with pytest.raises(DataSourceError, match="Unable to extract text from PDF"):
                handler._extract_pdf_text(b'%PDF-1.4...', 'test.pdf')

    def test_table_to_text_success(self, valid_config):
        """Test successful table conversion to text."""
        handler = StaticDataSourceHandler(valid_config)
        
        table = [
            ['Header1', 'Header2', 'Header3'],
            ['Row1Col1', 'Row1Col2', 'Row1Col3'],
            ['Row2Col1', 'Row2Col2', 'Row2Col3']
        ]
        
        result = handler._table_to_text(table)
        
        assert "Header1 | Header2 | Header3" in result
        assert "Row1Col1 | Row1Col2 | Row1Col3" in result

    def test_table_to_text_empty_table(self, valid_config):
        """Test table conversion with empty table."""
        handler = StaticDataSourceHandler(valid_config)
        
        assert handler._table_to_text([]) == ""
        assert handler._table_to_text(None) == ""

    def test_table_to_text_with_none_values(self, valid_config):
        """Test table conversion with None values."""
        handler = StaticDataSourceHandler(valid_config)
        
        table = [['A', None, 'C'], [None, 'B', None]]
        result = handler._table_to_text(table)
        
        assert "A |  | C" in result
        assert " | B | " in result

    @patch('autoindexer.data_source_handler.static_handler.chardet.detect')
    def test_decode_content_encoding_detection(self, mock_detect, valid_config):
        """Test content decoding with encoding detection."""
        mock_detect.return_value = {'encoding': 'utf-8', 'confidence': 0.9}
        
        handler = StaticDataSourceHandler(valid_config)
        content = "Test content with unicode: café"
        encoded_content = content.encode('utf-8')
        
        result = handler._decode_content(encoded_content, 'text/plain', 'test.txt')
        
        assert result == content

    def test_decode_content_content_type_encoding(self, valid_config):
        """Test content decoding using encoding from content-type header."""
        handler = StaticDataSourceHandler(valid_config)
        content = "Test content"
        encoded_content = content.encode('latin1')
        
        result = handler._decode_content(
            encoded_content, 
            'text/plain; charset=latin1', 
            'test.txt'
        )
        
        assert result == content

    def test_decode_content_empty_content(self, valid_config):
        """Test decoding of empty content."""
        handler = StaticDataSourceHandler(valid_config)
        
        result = handler._decode_content(b'', 'text/plain', 'empty.txt')
        
        assert result == ""

    def test_decode_content_fallback_with_errors(self, valid_config):
        """Test content decoding fallback when all encodings fail."""
        handler = StaticDataSourceHandler(valid_config)
        # Use bytes that can't be decoded properly
        invalid_content = b'\xff\xfe\x00\x00invalid'
        
        result = handler._decode_content(invalid_content, 'text/plain', 'test.txt')
        
        # Should use UTF-8 with error replacement
        assert isinstance(result, str)

    @patch('builtins.open', new_callable=mock_open, read_data='File content')
    @patch('os.path.exists', return_value=True)
    @patch('os.path.isfile', return_value=True)
    def test_read_file_content_success(self, mock_isfile, mock_exists, mock_file, valid_config):
        """Test successful file content reading."""
        handler = StaticDataSourceHandler(valid_config)
        
        result = handler._read_file_content('/path/to/file.txt')
        
        assert result == 'File content'

    @patch('os.path.exists', return_value=False)
    def test_read_file_content_not_found(self, mock_exists, valid_config):
        """Test file reading when file doesn't exist."""
        handler = StaticDataSourceHandler(valid_config)
        
        with pytest.raises(DataSourceError, match="File not found"):
            handler._read_file_content('/nonexistent/file.txt')

    @patch('os.path.exists', return_value=True)
    @patch('os.path.isfile', return_value=False)
    def test_read_file_content_not_file(self, mock_isfile, mock_exists, valid_config):
        """Test file reading when path is not a file."""
        handler = StaticDataSourceHandler(valid_config)
        
        with pytest.raises(DataSourceError, match="Path is not a file"):
            handler._read_file_content('/path/to/directory')

    @patch('builtins.open', new_callable=mock_open, read_data='')
    @patch('os.path.exists', return_value=True)
    @patch('os.path.isfile', return_value=True)
    def test_read_file_content_empty_file(self, mock_isfile, mock_exists, mock_file, valid_config):
        """Test reading empty file."""
        handler = StaticDataSourceHandler(valid_config)
        
        result = handler._read_file_content('/path/to/empty.txt')
        
        assert result is None

    def test_get_current_timestamp(self, valid_config):
        """Test timestamp generation."""
        handler = StaticDataSourceHandler(valid_config)
        
        timestamp = handler._get_current_timestamp()
        
        assert isinstance(timestamp, str)
        assert timestamp.endswith('Z')
        assert 'T' in timestamp  # ISO format should contain 'T'

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_with_code_language_detection(self, mock_get, mock_rag_client):
        """Test language detection for code files during index update."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "static": {
                "urls": ["https://example.com/script.py"]
            }
        }
        
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'print("Hello, World!")']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(config)
        errors = handler.update_index("test-index", mock_rag_client)
        
        assert errors == []
        
        call_args = mock_rag_client.index_documents.call_args
        documents = call_args[1]["documents"]
        assert len(documents) == 1
        assert documents[0]["metadata"]["language"] == "python"
        assert documents[0]["metadata"]["split_type"] == "code"