#!/usr/bin/env python3
"""
Test PDF extraction with various types of PDFs including GitHub-hosted ones.
"""

import logging

from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.data_source_handler.static_handler import StaticDataSourceHandler

# Configure logging
logging.basicConfig(level=logging.INFO)

def test_github_pdf():
    """Test PDF extraction from a GitHub raw file."""
    
    # Configuration for a PDF hosted on GitHub
    config = {
        "static": {
            "endpoints": [
                # Sample PDF from GitHub (you can replace with any public PDF URL)
                "https://www.adobe.com/content/dam/acom/en/devnet/pdf/pdfs/PDF32000_2008.pdf"
            ],
            "timeout": 60,
            "max_file_size": 20 * 1024 * 1024  # 20MB limit for larger PDFs
        }
    }
    
    try:
        print("Testing PDF extraction from a larger PDF...")
        handler = StaticDataSourceHandler(config)
        documents = handler.fetch_documents()
        
        if documents:
            print(f"\nSuccessfully processed {len(documents)} document(s)")
            for i, doc in enumerate(documents):
                text_content = doc['text']
                print(f"\nDocument {i+1}:")
                print(f"  Source: {doc['metadata']['source_url']}")
                print(f"  Text length: {len(text_content):,} characters")
                
                # Show a preview of the content
                lines = text_content.split('\n')
                print(f"  Number of lines: {len(lines)}")
                print("  Preview (first 300 characters):")
                print(f"    {text_content[:300].replace(chr(10), ' ')[:300]}...")
                
                # Show if it contains page markers
                page_count = text_content.count('--- Page')
                if page_count > 0:
                    print(f"  Contains {page_count} page markers")
                    
        else:
            print("No documents were processed")
            
    except DataSourceError as e:
        print(f"DataSource Error: {e}")
    except Exception as e:
        print(f"Unexpected error: {e}")

def test_multiple_formats():
    """Test various file formats including PDF and text."""
    
    config = {
        "static": {
            "endpoints": [
                "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf",  # PDF
                "https://raw.githubusercontent.com/microsoft/vscode/main/README.md",  # Markdown
            ],
            "timeout": 30,
            "max_file_size": 10 * 1024 * 1024
        }
    }
    
    try:
        print("\n" + "="*60)
        print("Testing multiple file formats (PDF + Markdown)...")
        handler = StaticDataSourceHandler(config)
        documents = handler.fetch_documents()
        
        if documents:
            print(f"\nSuccessfully processed {len(documents)} document(s)")
            for i, doc in enumerate(documents):
                print(f"\nDocument {i+1}:")
                print(f"  Source: {doc['metadata']['source_url']}")
                print(f"  Source type: {doc['metadata']['source_type']}")
                print(f"  Text length: {len(doc['text'])} characters")
                
                # Determine file type from URL
                url = doc['metadata']['source_url']
                if url.endswith('.pdf'):
                    print("  File type: PDF")
                elif url.endswith('.md'):
                    print("  File type: Markdown")
                else:
                    print("  File type: Other")
                    
                print(f"  Preview: {doc['text'][:150].replace(chr(10), ' ')[:150]}...")
                    
        else:
            print("No documents were processed")
            
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test_github_pdf()
    test_multiple_formats()