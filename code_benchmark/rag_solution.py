#!/usr/bin/env python3
"""
RAG-based issue resolution tool using /v1/chat/completions API.
Processes issues with RAG-enhanced context retrieval.
"""

import os
import sys
import json
import subprocess
import tempfile
import argparse
import re
from datetime import datetime
from pathlib import Path
from typing import List, Dict, Optional, Tuple, Set
import requests
import re


class RagResolver:
    def __init__(
        self,
        repo_path: str,
        rag_service_url: str,
        index_name: str,
        model: str = "deepseek-v3.1",
        head_lines: Optional[int] = None,
        api_timeout: int = 3600,
    ):
        """
        Initialize the RAG resolver.
        
        Args:
            repo_path: Path to the repository root
            rag_service_url: URL of the RAG service
            index_name: Name of the repository index in RAG service
            model: Model name to use
        """
        self.repo_path = Path(repo_path).resolve()
        self.rag_service_url = rag_service_url.rstrip('/')
        self.index_name = index_name
        self.model = model
        self.results = []
        # store last raw RAG response for debugging
        self.last_raw_response: Optional[str] = None
        self.head_lines = head_lines
        self.api_timeout = api_timeout
        
        
    def read_issues(self, issues_file: str) -> List[str]:
        """Read issues from a text file, one issue per line."""
        issues_path = Path(issues_file)
        if not issues_path.exists():
            print(f"❌ Error: Issues file not found: {issues_file}")
            sys.exit(1)
        
        with open(issues_path, 'r', encoding='utf-8') as f:
            issues = [line.strip() for line in f if line.strip()]
        
        print(f"📋 Loaded {len(issues)} issues from {issues_file}")
        return issues
    
    def call_rag(self, issue: str, file_contents: Dict[str, str]) -> Optional[Dict]:
        """
        Call RAG API to get code modifications with automatic context retrieval.
        
        Args:
            issue: Issue description
            file_contents: Ignored (RAG handles context automatically)
            
        Returns:
            RAG response with modifications
        """
        print(f"  🤖 Calling RAG API ({self.model})...")
        
        # Enhanced prompt with strict JSON format and import/test handling
        prompt = f"""Issue to resolve: {issue}

Instructions:
1. Analyze the provided files and the issue description
2. Determine which files need to be modified to resolve this specific issue
3. **CRITICAL - Import Handling**: Carefully manage import/dependency statements:
   - Add missing imports when you use new types/functions/modules
   - Keep existing imports that are still needed
   - Remove only imports that are truly unused
4. **CRITICAL - Test Files**: If modifying source code, also update corresponding test files when needed:
   - Update test cases to cover new functionality
   - Fix broken tests due to signature changes
   - Add new test cases for new features
5. Provide the COMPLETE modified file content for each file that needs changes
6. **CRITICAL - JSON Format**: Use proper JSON string format:
   - Use double quotes for strings: "content": "..."
   - Escape newlines as \\n, tabs as \\t, quotes as \\"
   - DO NOT use backticks (`) or template literals
   - DO NOT use multi-line strings without escaping

Response format (VALID JSON ONLY):
{{
  "files": [
    {{
      "path": "relative/path/to/file.go",
      "content": "package main\\n\\nimport (\\n\\t\\"fmt\\"\\n)\\n\\nfunc main() {{\\n\\tfmt.Println(\\"hello\\")\\n}}\\n"
    }}
  ],
  "explanation": "Brief explanation of changes"
}}

CRITICAL RULES:
- ALWAYS add missing imports, NEVER remove needed imports
- ALWAYS preserve file headers (copyright, license, package declarations)
- ALWAYS escape special characters in JSON strings (\\n, \\t, \\", \\\\)
- NEVER use backticks (`) in JSON - only double quotes (")
- Provide COMPLETE file content, not partial/diff format
- Ensure code compiles and tests pass"""

        return self._call_rag_api(prompt)
    
    def _call_rag_api(self, prompt: str) -> Optional[Dict]:
        """Call RAG service /v1/chat/completions API with automatic context retrieval."""
        import time
        
        for retry in range(3):
            try:
                response = requests.post(
                    f"{self.rag_service_url}/v1/chat/completions",
                    headers={
                        "Content-Type": "application/json"
                    },
                    json={
                        "model": self.model,
                        "messages": [
                            {"role": "system", "content": """You are an expert code modification assistant.

⚠️  ABSOLUTE REQUIREMENTS - VIOLATING THESE WILL MAKE THE CODE UNUSABLE:

1. **File Headers - DO NOT TOUCH** (CRITICAL):
   - The FIRST lines of EVERY file contain copyright/license headers
   - You MUST preserve these lines EXACTLY as they are
   - Example: "// Copyright (c) ...", "# Copyright ...", "/* Copyright ..."
   - ❌ NEVER delete these lines
   - ❌ NEVER modify these lines
   - ❌ NEVER skip these lines in your output
   
2. **Package/Module Declarations - DO NOT TOUCH** (CRITICAL):
   - After the copyright header, files have package/module declarations
   - Examples: "package v1beta1", "module mymodule", "namespace MyApp"
   - You MUST preserve these lines EXACTLY as they are
   - ❌ NEVER delete package declarations
   - ❌ NEVER modify package names
   - ❌ NEVER skip package declarations in your output
   
3. **Import Statements - BE CAREFUL** (CRITICAL):
   - After package declarations come import statements
   - You MUST preserve ALL existing import blocks
   - You MAY add new imports if needed for new code
   - ❌ NEVER delete the entire import section
   - ❌ NEVER remove imports that are still in use
   - ✅ ADD missing imports for new code you write
   
4. **Complete File Content** (CRITICAL):
   - You MUST return the COMPLETE file from line 1 to the end
   - Your output must start with the copyright header
   - Your output must include package declaration
   - Your output must include all imports
   - Your output must include all existing code
   - ❌ NEVER return only a portion of the file
   - ❌ NEVER skip the beginning of the file
   
5. **Test Files**:
   - Preserve existing test structure
   - Add new tests if needed
   - Update broken tests if needed
   
6. **JSON Format**:
   - Always respond with valid JSON
   - Escape special characters: \\n, \\t, \\", \\\\
   - NEVER use backticks (`) in JSON strings

⚠️  REMEMBER: If you delete the copyright header, package declaration, or imports, the code will NOT compile and will be rejected!"""},
                            {"role": "user", "content": prompt}
                        ],
                        "temperature": 0.0,
                        "max_tokens": 40000,
                        "reasoning": False,
                        "stream": False,
                        "context_token_ratio": 0.7,
                        "index_name": self.index_name
                    },
                    timeout=self.api_timeout
                )
                
                if response.status_code == 200:
                    result = response.json()
                    
                    if 'choices' in result and len(result['choices']) > 0:
                        message = result['choices'][0]['message']
                        
                        # Handle both content and reasoning_content fields (for deepseek-r1)
                        content = message.get('content', '')
                        reasoning_content = message.get('reasoning_content', '')
                        
                        # Use reasoning_content if content is empty (deepseek-r1 case)
                        if reasoning_content and not content:
                            content = reasoning_content
                        
                        if not content:
                            print(f"  ⚠️  No content found in message: {message}")
                            return None
                            
                        self.last_raw_response = content
                        
                        # Extract usage information from RAG API response
                        usage_info = result.get('usage')
                        if usage_info:
                            print(f"  📊 Token usage from RAG API response: "
                                  f"{usage_info.get('total_tokens', 0)} total "
                                  f"(prompt: {usage_info.get('prompt_tokens', 0)}, "
                                  f"completion: {usage_info.get('completion_tokens', 0)})")
                        
                        parsed_response = self._parse_rag_response(content)
                        
                        # Add usage info to the parsed response if available
                        if parsed_response and usage_info:
                            parsed_response['usage'] = usage_info
                        
                        # CRITICAL: Replace RAG-returned paths with real paths from metadata
                        if parsed_response and 'files' in parsed_response:
                            parsed_response = self._fix_file_paths_from_metadata(parsed_response, result)
                        
                        return parsed_response
                    
                    return None
                elif response.status_code == 500 and retry < 2:
                    print(f"  ⚠️  RAG API HTTP 500 error, retrying in {2 ** retry} seconds... (attempt {retry + 1}/3)")
                    time.sleep(2 ** retry)  # 指数退避: 1s, 2s
                    continue
                else:
                    print(f"  ✗ RAG API request failed: HTTP {response.status_code}")
                    print(f"    Response: {response.text}")
                    return None
                
            except requests.exceptions.RequestException as e:
                if retry < 2:
                    print(f"  ⚠️  RAG API connection error, retrying in {2 ** retry} seconds... (attempt {retry + 1}/3): {e}")
                    time.sleep(2 ** retry)
                    continue
                else:
                    print(f"  ✗ RAG API request failed: {e}")
                    return None
        
        return None
    
    def _parse_rag_response(self, content: str) -> Optional[Dict]:
        """Parse RAG response to extract JSON."""
        # Keep original for diagnostics
        raw = content
        # Common cleanup of code fences
        cleaned = re.sub(r'^```(?:json)?\s*', '', raw.strip(), flags=re.IGNORECASE)
        cleaned = re.sub(r'```\s*$', '', cleaned).strip()
        
        # Early cleanup: remove non-ASCII characters that often cause issues
        cleaned = re.sub(r'[^\x00-\x7F]', '', cleaned)

        # 1. Direct attempt
        for candidate in (cleaned, raw):
            # Also apply non-ASCII cleanup to raw if needed
            if candidate == raw:
                candidate = re.sub(r'[^\x00-\x7F]', '', candidate)
            try:
                return json.loads(candidate)
            except json.JSONDecodeError as e:
                if candidate == cleaned:
                    print(f"  🔍 JSON parse error: {e}")

        # 2. Try deepseek-r1 specific parsing (extract files manually)
        result = self._parse_deepseek_response(cleaned)
        if result:
            print("  ✅ Successfully parsed using deepseek-specific parser")
            return result

        # 3. Extract first JSON object heuristically
        # Find the earliest '{' and latest '}' and try substrings decreasing
        first_brace = cleaned.find('{')
        last_brace = cleaned.rfind('}')
        if first_brace != -1 and last_brace != -1 and last_brace > first_brace:
            possible = cleaned[first_brace:last_brace+1]
            
            # Try to fix common issues in the JSON
            # Non-ASCII characters already removed above
            possible = re.sub(r'\\n(?!["\]}])', '\\\\n', possible)  # Fix newlines
            
            try:
                return json.loads(possible)
            except json.JSONDecodeError:
                # Try with a more aggressive cleanup - truncate at last complete closing brace
                lines = possible.split('\n')
                for i in range(len(lines)-1, -1, -1):
                    if '}' in lines[i]:
                        truncated = '\n'.join(lines[:i+1])
                        if truncated.endswith('}'):
                            try:
                                return json.loads(truncated)
                            except json.JSONDecodeError:
                                continue
                        break

        print("  ⚠️  Failed to parse JSON response from RAG")
        preview = cleaned[:500].replace('\n', ' ')
        print(f"  Raw preview: {preview}...")
        return None
    
    def _parse_deepseek_response(self, content: str) -> Optional[Dict]:
        """Parse deepseek-r1 responses that may have malformed JSON but correct structure."""
        try:
            # Look for file patterns in the content
            files = []
            
            # Pattern: "path": "some/path", followed by "content": "..."
            path_pattern = r'"path"\s*:\s*"([^"]+)"'
            
            # Find all file paths
            path_matches = re.finditer(path_pattern, content)
            
            for path_match in path_matches:
                file_path = path_match.group(1)
                start_pos = path_match.end()
                
                # Look for the content field after this path
                content_pattern = r'"content"\s*:\s*"'
                content_match = re.search(content_pattern, content[start_pos:])
                
                if content_match:
                    content_start = start_pos + content_match.end()
                    
                    # Find the end of this content string (challenging with escaped quotes)
                    file_content = self._extract_string_content(content, content_start)
                    
                    if file_content is not None:
                        files.append({
                            "path": file_path,
                            "content": file_content
                        })
            
            if files:
                return {"files": files}
            
        except Exception as e:
            print(f"  🔍 Deepseek parser error: {e}")
        
        return None
    
    def _extract_string_content(self, text: str, start_pos: int) -> Optional[str]:
        """Extract string content from position, handling escaped quotes."""
        content_chars = []
        i = start_pos
        escape_next = False
        
        while i < len(text):
            char = text[i]
            
            if escape_next:
                # Handle escaped characters
                if char == 'n':
                    content_chars.append('\n')
                elif char == 't':
                    content_chars.append('\t')
                elif char == 'r':
                    content_chars.append('\r')
                elif char == '"':
                    content_chars.append('"')
                elif char == '\\':
                    content_chars.append('\\')
                else:
                    content_chars.append(char)
                escape_next = False
            elif char == '\\':
                escape_next = True
            elif char == '"':
                # End of string found
                return ''.join(content_chars)
            else:
                content_chars.append(char)
            
            i += 1
            
            # Safety check: don't parse forever
            if len(content_chars) > 50000:  # Max reasonable file size
                break
        
        return None
    
    def _fix_file_paths_from_metadata(self, parsed_response: Dict, rag_result: Dict) -> Dict:
        """
        Replace RAG-returned file paths with real paths from RAG metadata.
        Only keep the TOP 4 files with highest relevance scores.
        
        Args:
            parsed_response: Parsed RAG response with 'files' array
            rag_result: Full RAG API response containing source_nodes with metadata
            
        Returns:
            Updated parsed_response with corrected file paths
        """
        if 'files' not in parsed_response:
            return parsed_response
        
        # Extract real file paths from RAG metadata with relevance scores
        MAX_FILES = 4  # Only keep top 4 most relevant files
        
        file_path_scores = {}  # {normalized_path: score}
        source_nodes = rag_result.get('source_nodes', [])
        
        print(f"  📊 RAG returned {len(source_nodes)} source nodes")
        
        for node in source_nodes:
            score = node.get('score', 0.0)
            metadata = node.get('metadata', {})
            file_path = metadata.get('file_path') or metadata.get('absolute_path')
            
            if file_path:
                # Normalize path (remove leading ./ or /)
                normalized = file_path.lstrip('./')
                
                # Keep the highest score for each file
                if normalized not in file_path_scores or score > file_path_scores[normalized]:
                    file_path_scores[normalized] = score
        
        # Sort by score (highest first) and take top MAX_FILES
        sorted_files = sorted(file_path_scores.items(), key=lambda x: x[1], reverse=True)
        top_files = sorted_files[:MAX_FILES]
        
        # Print all files with selection status
        print(f"  📋 Relevance scores for all {len(sorted_files)} files:")
        for i, (path, score) in enumerate(sorted_files, 1):
            if i <= MAX_FILES:
                print(f"     ✓ TOP{i}: {score:.4f} | {path}")
            else:
                print(f"     ✗ {score:.4f} | {path}")
        
        if len(sorted_files) > MAX_FILES:
            print(f"  ✅ Selected TOP {MAX_FILES} files, filtered out {len(sorted_files) - MAX_FILES} lower-relevance files")
        
        real_paths = {path for path, score in top_files}
        
        if not real_paths:
            print(f"  ⚠️  No file paths found in RAG metadata, keeping RAG-returned paths")
            return parsed_response
        
        print(f"  📁 Found {len(real_paths)} real file paths from RAG metadata")
        
        # Match RAG-returned paths to real paths
        rag_files = parsed_response['files']
        fixed_files = []
        
        for rag_file in rag_files:
            rag_path = rag_file.get('path', '')
            
            # Try to find a matching real path
            matched_path = self._match_path_to_metadata(rag_path, real_paths)
            
            if matched_path:
                print(f"  ✅ Matched: {rag_path} -> {matched_path}")
                rag_file['path'] = matched_path
                rag_file['_original_rag_path'] = rag_path  # Keep for debugging
                fixed_files.append(rag_file)
            else:
                # Check if the RAG path actually exists
                import os
                if os.path.exists(rag_path):
                    print(f"  ✅ Keeping existing path: {rag_path}")
                    fixed_files.append(rag_file)
                else:
                    print(f"  ⚠️  No match found for RAG path: {rag_path}, trying to use it anyway")
                    # Keep the RAG path if no match found (might be a new file)
                    fixed_files.append(rag_file)
        
        parsed_response['files'] = fixed_files
        return parsed_response
    
    def _match_path_to_metadata(self, rag_path: str, real_paths: set) -> Optional[str]:
        """
        Match a RAG-returned path to a real path from metadata.
        
        Strategy:
        1. Exact match
        2. Basename exact match (highest priority)
        3. Same directory structure match
        4. Fuzzy keyword match (e.g., model.go -> test_model.go)
        """
        import os
        
        # Normalize RAG path
        rag_path = rag_path.lstrip('./')
        
        # 1. Exact match
        if rag_path in real_paths:
            return rag_path
        
        # Extract components from RAG path
        rag_basename = os.path.basename(rag_path)
        rag_name_without_ext = os.path.splitext(rag_basename)[0]  # e.g., "model" from "model.go"
        rag_ext = os.path.splitext(rag_basename)[1]  # e.g., ".go"
        rag_dir = os.path.dirname(rag_path)
        rag_dir_parts = rag_dir.split('/') if rag_dir else []
        
        candidates = []
        
        for real_path in real_paths:
            real_basename = os.path.basename(real_path)
            real_name_without_ext = os.path.splitext(real_basename)[0]
            real_ext = os.path.splitext(real_basename)[1]
            real_dir = os.path.dirname(real_path)
            real_dir_parts = real_dir.split('/') if real_dir else []
            
            score = 0
            
            # 2. Exact basename match (highest priority)
            if real_basename == rag_basename:
                score = 1000
            # 3. Same name, same extension (e.g., model.go -> interface.go won't match here)
            elif real_ext == rag_ext:
                # Keyword match in filename (e.g., model.go -> test_model.go)
                if rag_name_without_ext in real_name_without_ext:
                    score = 500
                elif real_name_without_ext in rag_name_without_ext:
                    score = 400
                # Check for common patterns (e.g., model vs interface in pkg/model/)
                elif rag_dir and real_dir:
                    # Same directory = likely the right file
                    if rag_dir == real_dir:
                        score = 800
                    # Parent directory match (pkg/model)
                    elif any(part in real_dir_parts for part in rag_dir_parts if part):
                        score = 300
                        # Boost if similar keywords
                        if rag_name_without_ext in real_name_without_ext or real_name_without_ext in rag_name_without_ext:
                            score += 200
            
            # 4. Directory structure match bonus
            if rag_dir_parts and real_dir_parts:
                matching_dir_parts = sum(1 for lp in rag_dir_parts if lp in real_dir_parts)
                score += matching_dir_parts * 50
            
            if score > 0:
                candidates.append((real_path, score))
        
        # Return the best match if score is good enough
        if candidates:
            candidates.sort(key=lambda x: x[1], reverse=True)
            best_path, best_score = candidates[0]
            
            # More lenient threshold - accept any reasonable match
            if best_score >= 300:
                return best_path
        
        return None
    
    def _format_raw_response(self, raw_response: str) -> str:
        """Format raw RAG response for better readability."""
        # First, clean up non-ASCII characters that cause issues
        cleaned = re.sub(r'[^\x00-\x7F]', '', raw_response)
        
        # Remove common code fence wrappers
        cleaned = re.sub(r'^```(?:json)?\s*', '', cleaned.strip(), flags=re.IGNORECASE)
        cleaned = re.sub(r'```\s*$', '', cleaned).strip()
        
        # Try to format as JSON if possible
        try:
            # Extract JSON part
            json_start = cleaned.find('{')
            json_end = cleaned.rfind('}')
            if json_start != -1 and json_end != -1:
                json_part = cleaned[json_start:json_end + 1]
                parsed = json.loads(json_part)
                formatted_json = json.dumps(parsed, indent=2, ensure_ascii=False)
                
                # Add header with metadata
                result = f"=== RAG Raw Response (Formatted JSON) ===\n"
                result += f"Generated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n"
                result += f"Original length: {len(raw_response)} chars\n"
                result += f"Cleaned length: {len(cleaned)} chars\n"
                result += f"Non-ASCII chars removed: {len(raw_response) - len(cleaned)}\n"
                if len(raw_response) - len(cleaned) > 0:
                    result += f"⚠️  Removed {len(raw_response) - len(cleaned)} non-ASCII characters that could cause parsing issues!\n"
                result += "=" * 50 + "\n\n"
                result += formatted_json
                return result
        except (json.JSONDecodeError, ValueError) as e:
            # If JSON parsing fails, show the error but still format nicely
            result = f"=== RAG Raw Response (JSON Parse Failed) ===\n"
            result += f"Generated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n"
            result += f"Original length: {len(raw_response)} chars\n"
            result += f"Cleaned length: {len(cleaned)} chars\n"
            result += f"Non-ASCII chars removed: {len(raw_response) - len(cleaned)}\n"
            result += f"JSON Parse Error: {e}\n"
            result += "=" * 50 + "\n\n"
            result += cleaned
            return result
        
        # If no JSON detected, just clean and return with header
        result = f"=== RAG Raw Response (No JSON Detected) ===\n"
        result += f"Generated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n"
        result += f"Original length: {len(raw_response)} chars\n"
        result += f"Cleaned length: {len(cleaned)} chars\n"
        result += f"Non-ASCII chars removed: {len(raw_response) - len(cleaned)}\n"
        result += "=" * 50 + "\n\n"
        result += cleaned
        return result
    
    def find_python_test_file(self, source_file: Path) -> Optional[Path]:
        """
        Find corresponding Python test file.
        Supports common Python test patterns.
        """
        file_name = source_file.stem
        file_dir = source_file.parent
        
        # Pattern 1: test_<name>.py in same directory
        test_file_1 = file_dir / f"test_{file_name}.py"
        if test_file_1.exists():
            return test_file_1
        
        # Pattern 2: <name>_test.py in same directory
        test_file_2 = file_dir / f"{file_name}_test.py"
        if test_file_2.exists():
            return test_file_2
        
        # Pattern 3: tests/ subdirectory
        tests_dir = file_dir / "tests"
        if tests_dir.exists():
            test_file_3 = tests_dir / f"test_{file_name}.py"
            if test_file_3.exists():
                return test_file_3
        
        # Pattern 4: test/ subdirectory
        test_dir = file_dir / "test"
        if test_dir.exists():
            test_file_4 = test_dir / f"test_{file_name}.py"
            if test_file_4.exists():
                return test_file_4
        
        # Pattern 5: parent's tests/ directory
        parent_tests = file_dir.parent / "tests"
        if parent_tests.exists():
            test_file_5 = parent_tests / f"test_{file_name}.py"
            if test_file_5.exists():
                return test_file_5
        
        return None
    
    def extract_package_from_file(self, file_path: Path) -> Optional[str]:
        """Extract the Go package path from a Go file."""
        if not file_path.suffix == '.go':
            return None
        
        package_dir = file_path.parent.relative_to(self.repo_path)
        
        if package_dir == Path('.'):
            return "./"
        
        return f"./{package_dir}"
    
    def extract_test_target_from_file(self, file_path: Path) -> Optional[Dict]:
        """Extract test target info from file based on language."""
        
        # Go files
        if file_path.suffix == '.go':
            package_dir = file_path.parent.relative_to(self.repo_path)
            pkg_path = "./" if package_dir == Path('.') else f"./{package_dir}"
            return {
                'language': 'go',
                'target': pkg_path,
                'type': 'package'
            }
        
        # Python files
        elif file_path.suffix == '.py':
            test_file = self.find_python_test_file(file_path)
            if test_file:
                return {
                    'language': 'python',
                    'target': str(test_file.relative_to(self.repo_path)),
                    'type': 'file'
                }
            else:
                return {
                    'language': 'python',
                    'target': str(file_path.relative_to(self.repo_path)),
                    'type': 'syntax_only'
                }
        
        return None
    
    def generate_diff(self, original_path: Path, new_content: str) -> str:
        """Generate unified diff between original file and new content."""
        if not original_path.exists():
            print(f"  ⚠️  Original file not found: {original_path}")
            return ""
        
        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.go') as tmp_file:
            tmp_file.write(new_content)
            tmp_path = tmp_file.name
        
        try:
            result = subprocess.run(
                ['diff', '-u', str(original_path), tmp_path],
                capture_output=True,
                text=True
            )
            
            diff_output = result.stdout
            diff_output = diff_output.replace(tmp_path, str(original_path))
            
            return diff_output
        finally:
            os.unlink(tmp_path)
    
    def apply_changes(self, modifications: Dict) -> Tuple[List[str], List[str]]:
        """Apply modifications to files."""
        modified_files = []
        diffs = []
        
        if 'files' not in modifications:
            print("  ⚠️  No 'files' key in modifications")
            return modified_files, diffs
        
        for file_info in modifications['files']:
            file_path = self.repo_path / file_info['path']
            new_content = file_info['content']
            
            if not file_path.exists():
                print(f"  ⚠️  File not found: {file_path}")
                continue
            
            # Generate diff before modifying
            diff = self.generate_diff(file_path, new_content)
            if diff:
                diffs.append(diff)
            
            # Apply changes
            with open(file_path, 'w', encoding='utf-8') as f:
                f.write(new_content)
            
            modified_files.append(str(file_path))
            print(f"  ✓ Modified: {file_info['path']}")
        
        return modified_files, diffs
    
    def run_go_tests(self, packages: List[str]) -> Dict:
        """Run Go tests for specified packages."""
        print(f"  🧪 Running Go tests for packages: {', '.join(packages)}")
        
        all_output = []
        all_passed = True
        tested_packages = []
        
        for pkg in packages:
            print(f"    Testing Go package {pkg}...")
            try:
                result = subprocess.run(
                    ['go', 'test', '-v', pkg],
                    cwd=self.repo_path,
                    capture_output=True,
                    text=True,
                    timeout=300
                )
                
                output = result.stdout + result.stderr
                all_output.append(f"=== Go Package: {pkg} ===\n{output}\n")
                
                if result.returncode != 0:
                    all_passed = False
                    print(f"    ✗ Go tests failed for {pkg}")
                else:
                    print(f"    ✓ Go tests passed for {pkg}")
                
                tested_packages.append(pkg)
                
            except subprocess.TimeoutExpired:
                print(f"    ⚠️  Go test timeout for {pkg}")
                all_output.append(f"=== Go Package: {pkg} ===\nTIMEOUT\n")
                all_passed = False
            except Exception as e:
                print(f"    ⚠️  Go test error for {pkg}: {e}")
                all_output.append(f"=== Go Package: {pkg} ===\nERROR: {e}\n")
                all_passed = False
        
        return {
            "status": "passed" if all_passed else "failed",
            "packages": tested_packages,
            "output": "\n".join(all_output)
        }
    
    def run_python_tests(self, targets: Set[Tuple[str, str]]) -> Dict:
        """Run Python tests for specified targets."""
        print(f"  🐍 Running Python tests...")
        
        all_output = []
        all_passed = True
        tested_files = []
        
        for target, test_type in targets:
            if test_type == 'syntax_only':
                print(f"    Checking Python syntax: {target}...")
                try:
                    result = subprocess.run(
                        ['python3', '-m', 'py_compile', target],
                        cwd=self.repo_path,
                        capture_output=True,
                        text=True,
                        timeout=30
                    )
                    
                    if result.returncode != 0:
                        all_passed = False
                        print(f"    ✗ Syntax error in {target}")
                        all_output.append(f"=== Python Syntax: {target} ===\n{result.stderr}\n")
                    else:
                        print(f"    ✓ Python syntax OK: {target}")
                        all_output.append(f"=== Python Syntax: {target} ===\nOK\n")
                        
                except Exception as e:
                    print(f"    ⚠️  Syntax check error: {e}")
                    all_output.append(f"=== Python Syntax: {target} ===\nERROR: {e}\n")
                    all_passed = False
                    
            elif test_type == 'file':
                print(f"    Testing Python file {target}...")
                try:
                    result = subprocess.run(
                        ['python3', '-m', 'pytest', target, '-v', '--tb=short'],
                        cwd=self.repo_path,
                        capture_output=True,
                        text=True,
                        timeout=300
                    )
                    
                    output = result.stdout + result.stderr
                    all_output.append(f"=== Python Test: {target} ===\n{output}\n")
                    
                    if result.returncode != 0:
                        all_passed = False
                        print(f"    ✗ Python tests failed for {target}")
                    else:
                        print(f"    ✓ Python tests passed for {target}")
                    
                    tested_files.append(target)
                    
                except FileNotFoundError:
                    print(f"    ⚠️  pytest not found, checking syntax only...")
                    result = subprocess.run(
                        ['python3', '-m', 'py_compile', target],
                        cwd=self.repo_path,
                        capture_output=True,
                        text=True
                    )
                    if result.returncode != 0:
                        all_passed = False
                        all_output.append(f"=== Python: {target} ===\nSyntax Error\n{result.stderr}\n")
                    else:
                        all_output.append(f"=== Python: {target} ===\nSyntax OK (pytest not available)\n")
                        
                except subprocess.TimeoutExpired:
                    print(f"    ⚠️  Python test timeout for {target}")
                    all_output.append(f"=== Python Test: {target} ===\nTIMEOUT\n")
                    all_passed = False
                except Exception as e:
                    print(f"    ⚠️  Python test error: {e}")
                    all_output.append(f"=== Python Test: {target} ===\nERROR: {e}\n")
                    all_passed = False
        
        return {
            "status": "passed" if all_passed else "failed",
            "files": list(tested_files),
            "output": "\n".join(all_output)
        }
    
    def run_tests(self, modified_files: List[str]) -> Dict:
        """Run tests for packages/files affected by modifications (multi-language)."""
        
        # Classify targets by language
        go_targets = set()
        python_targets = set()
        
        for file_path_str in modified_files:
            file_path = Path(file_path_str)
            test_info = self.extract_test_target_from_file(file_path)
            
            if test_info:
                if test_info['language'] == 'go':
                    go_targets.add(test_info['target'])
                elif test_info['language'] == 'python':
                    python_targets.add((test_info['target'], test_info['type']))
        
        if not go_targets and not python_targets:
            print("  ⚠️  No tests to run")
            return {"status": "skipped", "output": "No testable files modified"}
        
        results = {
            'overall_status': 'passed'
        }
        
        # Run Go tests
        if go_targets:
            results['go'] = self.run_go_tests(list(go_targets))
            if results['go']['status'] != 'passed':
                results['overall_status'] = 'failed'
        
        # Run Python tests
        if python_targets:
            results['python'] = self.run_python_tests(python_targets)
            if results['python']['status'] != 'passed':
                results['overall_status'] = 'failed'
        
        # Combine outputs for backward compatibility
        combined_output = []
        if 'go' in results:
            combined_output.append(results['go']['output'])
        if 'python' in results:
            combined_output.append(results['python']['output'])
        
        results['status'] = results['overall_status']
        results['output'] = "\n".join(combined_output)
        
        return results
    
    def revert_changes(self, modified_files: List[str]):
        """Revert changes to modified files using git."""
        if not modified_files:
            return
        
        print(f"  ↩️  Reverting {len(modified_files)} files...")
        
        try:
            subprocess.run(
                ['git', 'checkout'] + modified_files,
                cwd=self.repo_path,
                capture_output=True,
                check=True
            )
            print(f"  ✓ Changes reverted")
        except subprocess.CalledProcessError as e:
            print(f"  ⚠️  Failed to revert changes: {e}")
    
    def save_diff(self, issue_num: int, diffs: List[str], output_dir: Path):
        """Save diffs to a file."""
        if not diffs:
            return
        
        diff_file = output_dir / f"issue_{issue_num:03d}.diff"
        with open(diff_file, 'w', encoding='utf-8') as f:
            f.write("\n".join(diffs))
        
        print(f"  💾 Diff saved to: {diff_file}")
    
    def save_test_output(self, issue_num: int, test_results: Dict, output_dir: Path):
        """Save test output to a file."""
        if not test_results.get('output'):
            return
        
        test_file = output_dir / f"issue_{issue_num:03d}_tests.txt"
        with open(test_file, 'w', encoding='utf-8') as f:
            f.write(test_results['output'])
        
        print(f"  💾 Test output saved to: {test_file}")
    
    def process_issue(self, issue_num: int, issue: str, output_dir: Path) -> Dict:
        """Process a single issue: call RAG API, apply changes, run tests, revert."""
        
        print(f"\n{'='*80}")
        print(f"📝 RAG Issue #{issue_num}: {issue[:60]}{'...' if len(issue) > 60 else ''}")
        print(f"{'='*80}")
        
        result = {
            "issue_num": issue_num,
            "issue": issue,
            "status": "pending",
            "modified_files": [],
            "test_results": {},
            "error": None,
            "usage": None
        }
        
        # Call RAG API directly (no manual file reading needed)
        print(f"  🤖 Using RAG for automatic context retrieval...")
        modifications = self.call_rag(issue, {})  # Empty dict since RAG handles context
        if not modifications:
            result["status"] = "rag_failed"
            result["error"] = "Failed to get modifications from RAG API"
            # Save raw response if available
            if self.last_raw_response:
                raw_file = output_dir / f"issue_{issue_num:03d}_raw.txt"
                try:
                    # Format the raw response nicely
                    formatted_response = self._format_raw_response(self.last_raw_response)
                    with open(raw_file, 'w', encoding='utf-8') as rf:
                        rf.write(formatted_response)
                    print(f"  💾 Saved formatted raw RAG response to {raw_file}")
                except Exception as e:
                    print(f"  ⚠️  Could not save raw response: {e}")
            return result
        
        # Save usage information if available
        if modifications and 'usage' in modifications:
            result["usage"] = modifications['usage']
        
        # Apply changes
        modified_files, diffs = self.apply_changes(modifications)
        if not modified_files:
            result["status"] = "no_changes"
            result["error"] = "No files were modified"
            return result
        
        result["modified_files"] = modified_files
        
        # Save diff
        self.save_diff(issue_num, diffs, output_dir)
        
        # Run tests
        test_results = self.run_tests(modified_files)
        result["test_results"] = test_results
        result["status"] = test_results["status"]
        
        # Save test output
        self.save_test_output(issue_num, test_results, output_dir)
        
        # Revert changes
        self.revert_changes(modified_files)
        
        return result
    
    def run(self, issues_file: str, output_dir: str = "./rag_outputs"):
        """Main execution: process all issues."""
        print("="*80)
        print("🚀 RAG-Enhanced Issue Resolution")
        print("="*80)
        
        # Create output directory
        output_path = Path(output_dir)
        output_path.mkdir(exist_ok=True, parents=True)
        print(f"📁 Output directory: {output_path.resolve()}\n")
        
        # Read issues
        issues = self.read_issues(issues_file)
        if not issues:
            print("❌ No issues to process")
            return
        
        # Process each issue
        for idx, issue in enumerate(issues, 1):
            result = self.process_issue(idx, issue, output_path)
            self.results.append(result)
        
        # Generate summary report
        self.generate_summary_report(output_path)
    
    def generate_summary_report(self, output_dir: Path):
        """Generate a summary report of all issue resolutions."""
        print(f"\n{'='*80}")
        print("📊 RAG SUMMARY REPORT")
        print(f"{'='*80}\n")
        
        total = len(self.results)
        passed = sum(1 for r in self.results if r["status"] == "passed")
        failed = sum(1 for r in self.results if r["status"] == "failed")
        rag_failed = sum(1 for r in self.results if r["status"] == "rag_failed")
        no_changes = sum(1 for r in self.results if r["status"] == "no_changes")
        
        print(f"Total Issues:        {total}")
        print(f"Tests Passed:        {passed} ({passed/total*100:.1f}%)")
        print(f"Tests Failed:        {failed} ({failed/total*100:.1f}%)")
        print(f"RAG Failed:          {rag_failed}")
        print(f"No Changes:          {no_changes}")
        print()
        
        # Calculate tokens statistics
        total_prompt_tokens = 0
        total_completion_tokens = 0
        total_tokens = 0
        issues_with_usage = 0
        
        for result in self.results:
            usage = result.get("usage")
            if usage:
                issues_with_usage += 1
                total_prompt_tokens += usage.get("prompt_tokens", 0)
                total_completion_tokens += usage.get("completion_tokens", 0)
                total_tokens += usage.get("total_tokens", 0)
        
        if issues_with_usage > 0:
            print("Token Usage Statistics:")
            print("-" * 80)
            print(f"Issues with token data: {issues_with_usage}/{total}")
            print(f"Total Prompt Tokens:    {total_prompt_tokens:,}")
            print(f"Total Completion Tokens: {total_completion_tokens:,}")
            print(f"Total Tokens:           {total_tokens:,}")
            if issues_with_usage > 0:
                print(f"Average per Issue:      {total_tokens/issues_with_usage:.1f} tokens")
            print()
        else:
            print("Token Usage Statistics:")
            print("-" * 80)
            print("⚠️  No token usage data available")
            print("   This could mean:")
            print("   1. RAG service is not returning usage in API responses")
            print("   2. All issues failed before reaching the RAG service")
            print("   Check individual issue logs for details.")
            print()
        
        # Detailed results
        print("Detailed Results:")
        print("-" * 80)
        for result in self.results:
            status_emoji = {
                "passed": "✅",
                "failed": "❌",
                "rag_failed": "🔴",
                "no_changes": "⚠️"
            }.get(result["status"], "❓")
            
            print(f"{status_emoji} Issue #{result['issue_num']}: {result['issue'][:50]}...")
            if result.get("test_results", {}).get("packages"):
                print(f"   Tested packages: {', '.join(result['test_results']['packages'])}")
            if result.get("error"):
                print(f"   Error: {result['error']}")
            
            # Show individual token usage
            usage = result.get("usage")
            if usage:
                print(f"   Tokens: prompt={usage.get('prompt_tokens', 0)}, completion={usage.get('completion_tokens', 0)}, total={usage.get('total_tokens', 0)}")
            else:
                print(f"   Tokens: Not available (RAG service limitation)")
            print()
        
        # Save JSON report with tokens summary
        report_data = {
            "summary": {
                "total_issues": total,
                "tests_passed": passed,
                "tests_failed": failed,
                "rag_failed": rag_failed,
                "no_changes": no_changes,
                "success_rate": f"{passed/total*100:.1f}%" if total > 0 else "0%",
                "tokens_usage": {
                    "issues_with_data": issues_with_usage,
                    "total_prompt_tokens": total_prompt_tokens,
                    "total_completion_tokens": total_completion_tokens,
                    "total_tokens": total_tokens,
                    "average_tokens_per_issue": round(total_tokens/issues_with_usage, 1) if issues_with_usage > 0 else 0
                }
            },
            "issues": self.results
        }
        
        report_file = output_dir / "rag_summary_report.json"
        with open(report_file, 'w', encoding='utf-8') as f:
            json.dump(report_data, f, indent=2)
        
        print(f"💾 Full report saved to: {report_file}")
        print("="*80)


def main():
    parser = argparse.ArgumentParser(
        description='RAG-enhanced issue resolution using /v1/chat/completions API',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Using OpenAI API
  python resolve_issues_baseline.py --config issues_baseline.json --api-key sk-xxx --api-type openai --model gpt-4
  
  # Using environment variable for API key
  export LLM_API_KEY=sk-xxx
  python resolve_issues_baseline.py --config issues_baseline.json --api-type openai --model gpt-4
  
  # Using Anthropic API
  python resolve_issues_baseline.py --config issues_baseline.json --api-key xxx --api-type anthropic --model claude-3-opus-20240229

Config file format (issues_baseline.json):
[
  {
    "issue": "Fix GPU allocation bug in controllers",
    "files": [
      "controllers/rag_controller.go",
      "controllers/workspace_controller.go",
      "pkg/utils/resources.go"
    ]
  },
  {
    "issue": "Add validation for nil pointer",
    "files": [
      "pkg/utils/validator.go",
      "pkg/utils/validator_test.go"
    ]
  }
]
        """
    )
    
    parser.add_argument(
        '--issues',
        required=True,
        help='Path to text file containing issues (one issue per line)'
    )
    
    parser.add_argument(
        '--url',
        default='http://localhost:5000',
        help='RAG service URL (default: http://localhost:5000)'
    )
    
    parser.add_argument(
        '--index',
        required=True,
        help='Index name in RAG service'
    )
    
    parser.add_argument(
        '--model',
        default='deepseek-v3.1',
        help='Model name (default: deepseek-v3.1)'
    )
    
    parser.add_argument(
        '--repo',
        default='.',
        help='Repository path (default: current directory)'
    )
    
    parser.add_argument(
        '--output',
        default='./rag_outputs',
        help='Output directory (default: ./rag_outputs)'
    )

    parser.add_argument(
        '--head-lines',
        type=int,
        default=None,
        help='If set, only include the first N lines of each context file (reduces prompt size/timeouts)'
    )

    parser.add_argument(
        '--api-timeout',
        type=int,
        default=3600,
        help='HTTP timeout (seconds) for RAG API requests (default: 3600)'
    )
    
    args = parser.parse_args()
    
    # Validate repository path
    if not os.path.isdir(args.repo):
        print(f"❌ Error: Repository path does not exist: {args.repo}")
        sys.exit(1)
    
    # Check if it's a git repository
    git_dir = Path(args.repo) / '.git'
    if not git_dir.exists():
        print(f"❌ Error: Not a git repository: {args.repo}")
        sys.exit(1)
    
    # Create resolver and run
    resolver = RagResolver(
        args.repo,
        args.url,
        args.index,
        model=args.model,
        head_lines=args.head_lines,
        api_timeout=args.api_timeout,
    )
    resolver.run(args.issues, args.output)


if __name__ == "__main__":
    main()