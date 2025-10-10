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
Simple test script to verify PDF text extraction functionality.
"""

import logging

from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.data_source_handler.static_handler import StaticDataSourceHandler

# Configure logging
logging.basicConfig(level=logging.INFO)

def test_pdf_extraction():
    """Test PDF extraction with a sample PDF URL."""
    
    # Configuration for static data source with a sample PDF
    config = {
        "static": {
            "endpoints": [
                # Using a sample PDF from the web - this is a small test PDF
                "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf"
            ],
            "timeout": 30,
            "max_file_size": 5 * 1024 * 1024  # 5MB limit
        }
    }
    
    try:
        # Initialize the handler
        handler = StaticDataSourceHandler(config)
        
        print("Testing PDF text extraction...")
        documents = handler.fetch_documents()
        
        if documents:
            print(f"\nSuccessfully processed {len(documents)} document(s)")
            for i, doc in enumerate(documents):
                print(f"\nDocument {i+1}:")
                print(f"  Source: {doc['metadata']['source_url']}")
                print(f"  Text length: {len(doc['text'])} characters")
                print(f"  Preview: {doc['text'][:200]}...")
        else:
            print("No documents were processed")
            
    except DataSourceError as e:
        print(f"DataSource Error: {e}")
    except Exception as e:
        print(f"Unexpected error: {e}")

if __name__ == "__main__":
    test_pdf_extraction()