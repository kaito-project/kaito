import contextlib
import io
import json
import logging
import os
from typing import Any
from urllib.parse import urlparse

import chardet
import requests

from autoindexer.data_source_handler.handler import (
    DataSourceError,
    DataSourceHandler,
)

logger = logging.getLogger(__name__)


class StaticDataSourceHandler(DataSourceHandler):
    """
    Handler for static data sources (direct file URLs or content).
    
    This handler supports:
    - HTTP/HTTPS URLs pointing to text files (.txt, .md, .rst, etc.)
    - PDF files with automatic text extraction (.pdf)
    - Configuration files (.json, .yaml, .toml, etc.)
    - Source code files (.py, .go, .js, etc.)
    - Raw files from GitHub, GitLab, and other repositories
    - Direct text content provided in configuration
    
    PDF Processing Features:
    - Automatic text extraction from PDF files using PyPDF2 and pdfplumber
    - Table extraction from PDFs (when using pdfplumber)
    - Multi-page document support with page markers
    - Fallback between extraction methods for better compatibility
    - Configurable file size limits for PDF downloads
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
        
        # Handle URLs
        if "endpoints" in self.static_config:
            url_list = self.static_config["endpoints"]
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
        
        if not documents:
            logger.warning("No documents found in static data source configuration")
        else:
            logger.info(f"Total documents fetched from static data source: {len(documents)}")
        
        return documents

    def _fetch_content_from_url(self, url: str) -> str | None:
        """
        Fetch content from a URL, optimized for downloading files.
        
        Supports various file types including:
        - Raw text files from GitHub, GitLab, etc.
        - Documentation files (.md, .txt, .rst)
        - Configuration files (.json, .yaml, .toml)
        - Source code files (.py, .go, .js, etc.)
        
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
            
            # Determine if this looks like a file URL
            is_file_url = self._is_file_url(url, parsed_url)
            
            # Prepare request headers optimized for file downloads
            headers = {
                'User-Agent': 'KAITO-AutoIndexer/1.0',
                'Accept': 'text/plain, text/markdown, text/x-markdown, application/json, text/html, */*',
                'Accept-Encoding': 'gzip, deflate, br',
                'Cache-Control': 'no-cache'
            }
            
            # # Add GitHub-specific headers for better API integration
            # if 'github.com' in parsed_url.netloc or 'raw.githubusercontent.com' in parsed_url.netloc:
            #     headers['Accept'] = 'application/vnd.github.v3.raw+json, text/plain, */*'
            #     # Add GitHub token if available
            #     if self.credentials and 'github_token' in self.credentials:
            #         headers['Authorization'] = f"token {self.credentials['github_token']}"
            
            # # Add GitLab-specific headers
            # elif 'gitlab.com' in parsed_url.netloc or 'gitlab' in parsed_url.netloc:
            #     if self.credentials and 'gitlab_token' in self.credentials:
            #         headers['Private-Token'] = self.credentials['gitlab_token']
            
            # Add authentication if available
            auth = None
            if self.credentials:
                if "http_auth" in self.credentials:
                    auth_config = self.credentials["http_auth"]
                    if "username" in auth_config and "password" in auth_config:
                        auth = (auth_config["username"], auth_config["password"])
                
                if "headers" in self.credentials:
                    headers.update(self.credentials["headers"])
            
            # Make request with timeout and streaming for large files
            timeout = self.static_config.get("timeout", 60)  # Increased timeout for file downloads
            max_size = self.static_config.get("max_file_size", 10 * 1024 * 1024)  # 10MB default limit
            
            logger.info(f"Fetching content from {url} (file_url={is_file_url})")
            
            with requests.get(url, headers=headers, auth=auth, timeout=timeout, stream=True) as response:
                response.raise_for_status()
                
                # Check content length
                content_length = response.headers.get('content-length')
                if content_length and int(content_length) > max_size:
                    raise DataSourceError(f"File too large: {content_length} bytes exceeds limit of {max_size} bytes")
                
                # Handle different content types and file extensions
                content_type = response.headers.get('content-type', '').lower()
                
                # Download content with size limit protection
                content_chunks = []
                total_size = 0
                
                for chunk in response.iter_content(chunk_size=8192, decode_unicode=False):
                    if chunk:
                        total_size += len(chunk)
                        if total_size > max_size:
                            raise DataSourceError(f"File too large: exceeds limit of {max_size} bytes")
                        content_chunks.append(chunk)
                
                # Combine chunks into bytes
                raw_content = b''.join(content_chunks)
                
                # Check if this is a PDF file
                if self._is_pdf_content(raw_content, content_type, url):
                    return self._extract_pdf_text(raw_content, url)
                
                # Handle encoding detection and text conversion for non-PDF content
                return self._decode_content(raw_content, content_type, url)
                
        except requests.RequestException as e:
            logger.error(f"HTTP error fetching {url}: {e}")
            raise DataSourceError(f"HTTP error fetching {url}: {e}")
        except Exception as e:
            logger.error(f"Error fetching content from {url}: {e}")
            raise DataSourceError(f"Error fetching content from {url}: {e}")

    def _is_file_url(self, url: str, parsed_url) -> bool:
        """
        Determine if a URL likely points to a file rather than a web page.
        
        Args:
            url: The original URL string
            parsed_url: Parsed URL object
            
        Returns:
            bool: True if the URL appears to be a file
        """
        # Check for common file extensions
        file_extensions = {
            '.txt', '.md', '.rst', '.json', '.yaml', '.yml', '.toml', '.ini',
            '.py', '.go', '.js', '.ts', '.java', '.cpp', '.c', '.h',
            '.html', '.htm', '.xml', '.csv', '.tsv', '.log',
            '.sh', '.bat', '.ps1', '.dockerfile', '.gitignore',
            '.conf', '.cfg', '.properties', '.env', '.pdf'
        }
        
        path = parsed_url.path.lower()
        
        # Direct file extension match
        for ext in file_extensions:
            if path.endswith(ext):
                return True
        
        # GitHub raw URLs
        if 'raw.githubusercontent.com' in parsed_url.netloc:
            return True
        
        # GitLab raw URLs
        if 'gitlab' in parsed_url.netloc and '/raw/' in path:
            return True
        
        # GitHub blob URLs (convert to raw)
        return bool('github.com' in parsed_url.netloc and '/blob/' in path)

    def _is_pdf_content(self, raw_content: bytes, content_type: str, url: str) -> bool:
        """
        Determine if the content is a PDF file.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            url: Original URL for context
            
        Returns:
            bool: True if the content appears to be a PDF file
        """
        # Check content type
        if 'application/pdf' in content_type.lower():
            return True
        
        # Check URL extension
        if url.lower().endswith('.pdf'):
            return True
        
        # Check PDF magic bytes (PDF files start with %PDF)
        return bool(raw_content.startswith(b'%PDF'))

    def _extract_pdf_text(self, raw_content: bytes, url: str) -> str:
        """
        Extract text content from PDF bytes.
        
        Args:
            raw_content: Raw PDF bytes
            url: Original URL for context
            
        Returns:
            str: Extracted text content from the PDF
        """
        extracted_text = ""
        
        # Try PyPDF2 first (lighter weight)
        try:
            import PyPDF2
            
            pdf_stream = io.BytesIO(raw_content)
            pdf_reader = PyPDF2.PdfReader(pdf_stream)
            
            logger.info(f"PDF from {url} has {len(pdf_reader.pages)} pages")
            
            text_parts = []
            for page_num, page in enumerate(pdf_reader.pages, 1):
                try:
                    page_text = page.extract_text()
                    if page_text.strip():
                        text_parts.append(f"--- Page {page_num} ---\n{page_text.strip()}")
                        logger.debug(f"Extracted {len(page_text)} characters from page {page_num}")
                except Exception as e:
                    logger.warning(f"Failed to extract text from page {page_num} of {url}: {e}")
                    continue
            
            if text_parts:
                extracted_text = "\n\n".join(text_parts)
                logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF using PyPDF2")
            else:
                logger.warning(f"No text extracted from PDF {url} using PyPDF2, trying alternative method")
                
        except ImportError:
            logger.debug("PyPDF2 not available")
        except Exception as e:
            logger.warning(f"PyPDF2 extraction failed for {url}: {e}, trying alternative method")
        
        # If PyPDF2 didn't work or extracted no text, try pdfplumber
        if not extracted_text.strip():
            try:
                import pdfplumber
                
                pdf_stream = io.BytesIO(raw_content)
                
                with pdfplumber.open(pdf_stream) as pdf:
                    logger.info(f"PDF from {url} has {len(pdf.pages)} pages (pdfplumber)")
                    
                    text_parts = []
                    for page_num, page in enumerate(pdf.pages, 1):
                        try:
                            page_text = page.extract_text()
                            if page_text and page_text.strip():
                                text_parts.append(f"--- Page {page_num} ---\n{page_text.strip()}")
                                logger.debug(f"Extracted {len(page_text)} characters from page {page_num} using pdfplumber")
                            
                            # Also try to extract tables if available
                            tables = page.extract_tables()
                            if tables:
                                for table_num, table in enumerate(tables, 1):
                                    try:
                                        # Convert table to text format
                                        table_text = self._table_to_text(table)
                                        if table_text.strip():
                                            text_parts.append(f"--- Page {page_num} Table {table_num} ---\n{table_text.strip()}")
                                    except Exception as e:
                                        logger.debug(f"Failed to extract table {table_num} from page {page_num}: {e}")
                                        
                        except Exception as e:
                            logger.warning(f"Failed to extract text from page {page_num} of {url} using pdfplumber: {e}")
                            continue
                    
                    if text_parts:
                        extracted_text = "\n\n".join(text_parts)
                        logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF using pdfplumber")
                    
            except ImportError:
                logger.debug("pdfplumber not available")
            except Exception as e:
                logger.error(f"pdfplumber extraction failed for {url}: {e}")
        
        # Final validation
        if not extracted_text.strip():
            logger.error(f"Failed to extract any text from PDF {url}")
            raise DataSourceError(f"Unable to extract text from PDF: {url}")
        
        logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF {url}")
        return extracted_text

    def _table_to_text(self, table: list) -> str:
        """
        Convert a table (list of lists) to readable text format.
        
        Args:
            table: Table data as list of lists
            
        Returns:
            str: Formatted table as text
        """
        if not table:
            return ""
        
        try:
            # Filter out empty rows
            rows = [row for row in table if row and any(cell for cell in row if cell)]
            
            if not rows:
                return ""
            
            # Convert to strings and handle None values
            text_rows = []
            for row in rows:
                text_row = []
                for cell in row:
                    if cell is None:
                        text_row.append("")
                    else:
                        text_row.append(str(cell).strip())
                text_rows.append(text_row)
            
            # Create simple text table
            if text_rows:
                # Use pipe separators for simple table format
                table_lines = []
                for row in text_rows:
                    table_lines.append(" | ".join(row))
                return "\n".join(table_lines)
            
        except Exception as e:
            logger.debug(f"Error formatting table: {e}")
        
        return ""

    def _decode_content(self, raw_content: bytes, content_type: str, url: str) -> str:
        """
        Decode raw content bytes to string, handling various encodings.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            url: Original URL for context
            
        Returns:
            str: Decoded text content
        """
        # If content is empty
        if not raw_content:
            logger.warning(f"Empty content received from {url}")
            return ""
        
        # Try to determine encoding from content-type header
        encoding = None
        if 'charset=' in content_type:
            with contextlib.suppress(Exception):
                encoding = content_type.split('charset=')[1].split(';')[0].strip()
        
        # List of encodings to try in order
        encodings_to_try = []
        
        if encoding:
            encodings_to_try.append(encoding)
        
        # Add common encodings
        encodings_to_try.extend(['utf-8', 'utf-8-sig', 'latin1', 'cp1252', 'iso-8859-1'])
        
        # Try chardet detection if available
        try:
            detected = chardet.detect(raw_content)
            if detected and detected.get('encoding') and detected.get('confidence', 0) > 0.7:
                detected_encoding = detected['encoding']
                if detected_encoding not in encodings_to_try:
                    encodings_to_try.insert(1, detected_encoding)
        except Exception as e:
            logger.debug(f"Chardet detection failed for {url}: {e}")
        
        # Try each encoding
        for enc in encodings_to_try:
            try:
                decoded_content = raw_content.decode(enc)
                logger.debug(f"Successfully decoded content from {url} using encoding: {enc}")
                
                # Handle specific content types
                if 'application/json' in content_type:
                    try:
                        json_data = json.loads(decoded_content)
                        # Try to extract text content from JSON
                        if isinstance(json_data, dict):
                            if 'content' in json_data:
                                return str(json_data['content'])
                            elif 'text' in json_data:
                                return str(json_data['text'])
                            elif 'body' in json_data:
                                return str(json_data['body'])
                        # Return formatted JSON as string
                        return json.dumps(json_data, indent=2, ensure_ascii=False)
                    except json.JSONDecodeError:
                        # If JSON parsing fails, return as plain text
                        pass
                
                return decoded_content
                
            except UnicodeDecodeError:
                continue
            except Exception as e:
                logger.debug(f"Failed to decode with {enc}: {e}")
                continue
        
        # If all encodings fail, try with error handling
        try:
            content = raw_content.decode('utf-8', errors='replace')
            logger.warning(f"Used UTF-8 with error replacement for {url}")
            return content
        except Exception as e:
            logger.error(f"Failed to decode content from {url}: {e}")
            raise DataSourceError(f"Unable to decode content from {url}: {e}")

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