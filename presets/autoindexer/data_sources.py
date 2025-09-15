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

import logging
import os
from abc import ABC, abstractmethod
from typing import Any
from urllib.parse import urlparse

import requests


logger = logging.getLogger(__name__)


class DataSourceError(Exception):
    """Exception raised for data source related errors."""
    pass


class DataSourceHandler(ABC):
    """Abstract base class for data source handlers."""
    
    @abstractmethod
    def fetch_documents(self) -> list[dict[str, Any]]:
        """
        Fetch documents from the data source.
        
        Returns:
            list[dict[str, Any]]: List of documents with 'text' and optional 'metadata'
        """
        pass


class StaticDataSourceHandler(DataSourceHandler):
    """
    Handler for static data sources (direct file URLs or content).
    
    This handler supports:
    - HTTP/HTTPS URLs pointing to text files
    - Direct text content provided in configuration
    """
    
    def __init__(self, config: dict[str, Any], credentials: dict[str, Any] | None = None):
        """
        Initialize the static data source handler.
        
        Args:
            config: Configuration dictionary containing static data source settings
            credentials: Optional credentials for accessing the data source
        """
        self.config = config
        self.credentials = credentials or {}
        
        # Validate required configuration
        if not self.config.get("static"):
            raise DataSourceError("Static data source configuration is missing 'static' section")
        
        self.static_config = self.config["static"]
        logger.info(f"Initialized static data source handler with config: {self.static_config}")

    def fetch_documents(self) -> list[dict[str, Any]]:
        """
        Fetch documents from static data sources.
        
        Returns:
            list[dict[str, Any]]: List of documents with 'text' and optional 'metadata'
        """
        documents = []
        
        # Handle direct content
        if "content" in self.static_config:
            content_list = self.static_config["content"]
            if isinstance(content_list, str):
                content_list = [content_list]
            
            for i, content in enumerate(content_list):
                documents.append({
                    "text": content,
                    "metadata": {
                        "source_type": "direct_content",
                        "content_index": i,
                        "timestamp": self._get_current_timestamp()
                    }
                })
            logger.info(f"Added {len(content_list)} direct content documents")
        
        # Handle URLs
        if "urls" in self.static_config:
            url_list = self.static_config["urls"]
            if isinstance(url_list, str):
                url_list = [url_list]
            
            for url in url_list:
                try:
                    content = self._fetch_content_from_url(url)
                    if content:
                        documents.append({
                            "text": content,
                            "metadata": {
                                "source_type": "url",
                                "source_url": url,
                                "timestamp": self._get_current_timestamp()
                            }
                        })
                        logger.info(f"Successfully fetched content from {url}")
                    else:
                        logger.warning(f"No content retrieved from {url}")
                except Exception as e:
                    logger.error(f"Failed to fetch content from {url}: {e}")
                    raise DataSourceError(f"Failed to fetch content from {url}: {e}")
        
        # Handle file paths (if provided)
        if "file_paths" in self.static_config:
            file_paths = self.static_config["file_paths"]
            if isinstance(file_paths, str):
                file_paths = [file_paths]
            
            for file_path in file_paths:
                try:
                    content = self._read_file_content(file_path)
                    if content:
                        documents.append({
                            "text": content,
                            "metadata": {
                                "source_type": "file",
                                "file_path": file_path,
                                "timestamp": self._get_current_timestamp()
                            }
                        })
                        logger.info(f"Successfully read content from {file_path}")
                    else:
                        logger.warning(f"No content in file {file_path}")
                except Exception as e:
                    logger.error(f"Failed to read file {file_path}: {e}")
                    raise DataSourceError(f"Failed to read file {file_path}: {e}")
        
        if not documents:
            logger.warning("No documents found in static data source configuration")
        else:
            logger.info(f"Total documents fetched from static data source: {len(documents)}")
        
        return documents

    def _fetch_content_from_url(self, url: str) -> str | None:
        """
        Fetch content from a URL.
        
        Args:
            url: The URL to fetch content from
            
        Returns:
            str | None: The content of the URL or None if failed
        """
        try:
            # Parse URL to validate
            parsed_url = urlparse(url)
            if not parsed_url.scheme or not parsed_url.netloc:
                raise DataSourceError(f"Invalid URL format: {url}")
            
            # Prepare request headers
            headers = {
                'User-Agent': 'KAITO-AutoIndexer/1.0',
                'Accept': 'text/plain, text/html, application/json, */*'
            }
            
            # Add authentication if available
            auth = None
            if self.credentials:
                if "http_auth" in self.credentials:
                    auth_config = self.credentials["http_auth"]
                    if "username" in auth_config and "password" in auth_config:
                        auth = (auth_config["username"], auth_config["password"])
                
                if "headers" in self.credentials:
                    headers.update(self.credentials["headers"])
            
            # Make request with timeout
            timeout = self.static_config.get("timeout", 30)
            response = requests.get(url, headers=headers, auth=auth, timeout=timeout)
            response.raise_for_status()
            
            # Handle different content types
            content_type = response.headers.get('content-type', '').lower()
            
            if 'application/json' in content_type:
                # If JSON, try to extract text content
                try:
                    json_data = response.json()
                    if isinstance(json_data, dict) and 'text' in json_data:
                        return json_data['text']
                    elif isinstance(json_data, dict) and 'content' in json_data:
                        return json_data['content']
                    else:
                        # Return JSON as formatted string
                        return response.text
                except Exception:
                    return response.text
            else:
                # Return as text
                return response.text
                
        except requests.RequestException as e:
            logger.error(f"HTTP error fetching {url}: {e}")
            raise DataSourceError(f"HTTP error fetching {url}: {e}")
        except Exception as e:
            logger.error(f"Error fetching content from {url}: {e}")
            raise DataSourceError(f"Error fetching content from {url}: {e}")

    def _read_file_content(self, file_path: str) -> str | None:
        """
        Read content from a local file.
        
        Args:
            file_path: Path to the file to read
            
        Returns:
            str | None: The content of the file or None if failed
        """
        try:
            if not os.path.exists(file_path):
                raise DataSourceError(f"File not found: {file_path}")
            
            if not os.path.isfile(file_path):
                raise DataSourceError(f"Path is not a file: {file_path}")
            
            with open(file_path, encoding='utf-8') as f:
                content = f.read()
            
            if not content.strip():
                logger.warning(f"File {file_path} is empty")
                return None
            
            return content
            
        except Exception as e:
            logger.error(f"Error reading file {file_path}: {e}")
            raise DataSourceError(f"Error reading file {file_path}: {e}")

    def _get_current_timestamp(self) -> str:
        """Get current timestamp in ISO format."""
        from datetime import datetime
        return datetime.utcnow().isoformat() + 'Z'


class GitDataSourceHandler(DataSourceHandler):
    """
    Handler for Git data sources.
    
    This handler supports:
    - Cloning Git repositories
    - Checking out specific branches or commits
    - Reading files from the repository
    
    NOTE: This is a placeholder for future implementation
    """
    
    def __init__(self, config: dict[str, Any], credentials: dict[str, Any] | None = None):
        """Initialize the Git data source handler."""
        self.config = config
        self.credentials = credentials or {}
        raise NotImplementedError("Git data source handler is not yet implemented")
    
    def fetch_documents(self) -> list[dict[str, Any]]:
        """Fetch documents from Git repositories."""
        raise NotImplementedError("Git data source handler is not yet implemented")
