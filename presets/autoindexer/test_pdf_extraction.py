#!/usr/bin/env python3
"""
Simple test script to verify PDF text extraction functionality.
"""

import logging
from data_sources import StaticDataSourceHandler, DataSourceError

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