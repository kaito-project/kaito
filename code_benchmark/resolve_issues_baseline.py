#!/usr/bin/env python3
"""
Baseline issue resolution tool using direct LLM API (without RAG).
Processes issues with manually specified file contexts.
"""

import os
import sys
import json
import subprocess
import tempfile
import argparse
import re
import glob
from datetime import datetime
from pathlib import Path
from typing import List, Dict, Optional, Tuple, Set
import requests
import re


class BaselineResolver:
    def __init__(
        self,
        repo_path: str,
        api_key: str,
        api_type: str = "openai",
        model: str = "gpt-4",
        api_url: Optional[str] = None,
        head_lines: Optional[int] = None,
        api_timeout: int = 300,
    ):
        """
        Initialize the baseline resolver.
        
        Args:
            repo_path: Path to the repository root
            api_key: API key for the LLM service
            api_type: Type of API (openai, anthropic, etc.)
            model: Model name to use
        """
        self.repo_path = Path(repo_path).resolve()
        self.api_key = api_key
        self.api_type = api_type.lower()
        self.model = model
        self.results = []
        # store last raw LLM response for debugging
        self.last_raw_response: Optional[str] = None
        # store last token usage for reporting
        self.last_token_usage: Optional[Dict] = None
        self.head_lines = head_lines
        self.api_timeout = api_timeout
        
        # API endpoints (can be overridden by --api-url)
        self.api_endpoints = {
            "openai": "https://api.openai.com/v1/chat/completions",
            "anthropic": "https://api.anthropic.com/v1/messages",
        }

        if api_url:
            # If user supplies a full URL, just override the current api_type endpoint.
            # We don't attempt to construct the path; assume user passed the correct full endpoint.
            self.api_endpoints[self.api_type] = api_url.rstrip()
            print(f"🔧 Using custom API URL for {self.api_type}: {self.api_endpoints[self.api_type]}")
    
    def read_issues_config(self, config_file: str) -> List[Dict]:
        """
        Read issues configuration from JSON file.
        
        Expected format:
        [
          {
            "issue": "Fix GPU allocation bug",
            "files": ["controllers/rag_controller.go", "pkg/utils/resources.go"]
          }
        ]
        """
        config_path = Path(config_file)
        if not config_path.exists():
            print(f"❌ Error: Config file not found: {config_file}")
            sys.exit(1)
        
        with open(config_path, 'r', encoding='utf-8') as f:
            config = json.load(f)
        
        print(f"📋 Loaded {len(config)} issues from {config_file}")
        return config
    
    def scan_folder_for_files(self, folder_path: str, extensions: List[str] = ['.go', '.py']) -> List[str]:
        """
        Recursively scan a folder for files with specified extensions.
        
        Args:
            folder_path: Relative path to the folder to scan
            extensions: List of file extensions to include (default: ['.go', '.py'])
            
        Returns:
            List of relative file paths found in the folder
        """
        full_folder_path = self.repo_path / folder_path
        if not full_folder_path.exists():
            print(f"  ⚠️  Folder not found: {folder_path}")
            return []
        
        if not full_folder_path.is_dir():
            print(f"  ⚠️  Path is not a directory: {folder_path}")
            return []
        
        files = []
        for ext in extensions:
            # Use glob to find files recursively
            pattern = str(full_folder_path / f"**/*{ext}")
            found_files = glob.glob(pattern, recursive=True)
            for file_path in found_files:
                # Convert back to relative path
                rel_path = Path(file_path).relative_to(self.repo_path)
                files.append(str(rel_path))
        
        # Sort files for consistent ordering
        files.sort()
        print(f"  📂 Found {len(files)} files in {folder_path}: {extensions}")
        for file in files:
            print(f"    - {file}")
        
        return files
    
    def read_file_contents(self, file_paths: List[str]) -> Dict[str, str]:
        """
        Read contents of specified files.
        
        Args:
            file_paths: List of relative file paths
            
        Returns:
            Dictionary mapping file paths to their contents
        """
        contents = {}
        for file_path in file_paths:
            full_path = self.repo_path / file_path
            if not full_path.exists():
                print(f"  ⚠️  File not found: {file_path}")
                continue

            try:
                if self.head_lines and self.head_lines > 0:
                    # Stream only first N lines
                    collected_lines = []
                    with open(full_path, 'r', encoding='utf-8') as f:
                        for i, line in enumerate(f, 1):
                            collected_lines.append(line)
                            if i >= self.head_lines:
                                break
                    truncated_note = f"\n/* Truncated to first {self.head_lines} lines for brevity */\n"
                    contents[file_path] = "".join(collected_lines) + truncated_note
                    print(f"  ✓ Loaded (truncated to {self.head_lines} lines): {file_path} ({len(contents[file_path])} chars)")
                else:
                    with open(full_path, 'r', encoding='utf-8') as f:
                        contents[file_path] = f.read()
                    print(f"  ✓ Loaded: {file_path} ({len(contents[file_path])} chars)")
            except Exception as e:
                print(f"  ⚠️  Error reading {file_path}: {e}")
        
        return contents
    
    def call_llm(self, issue: str, file_contents: Dict[str, str]) -> Optional[Dict]:
        """
        Call LLM API to get code modifications.
        
        Args:
            issue: Issue description
            file_contents: Dictionary of file paths to contents
            
        Returns:
            LLM response with modifications
        """
        print(f"  🤖 Calling {self.api_type} API ({self.model})...")
        
        # Build context from files
        context = "Here are the relevant files:\n\n"
        for file_path, content in file_contents.items():
            context += f"=== File: {file_path} ===\n{content}\n\n"
        
        prompt = f"""{context}

Issue to resolve: {issue}

Instructions:
1. Analyze the provided files and the issue description
2. Determine which files need to be modified
3. Provide the COMPLETE modified file content for each file that needs changes
4. Format your response as JSON with this structure:
{{
  "files": [
    {{
      "path": "relative/path/to/file.go",
      "content": "complete modified file content here..."
    }}
  ],
  "explanation": "Brief explanation of changes"
}}

Important: 
- Provide COMPLETE file content, not just the changes
- Only include files that actually need modifications
- Ensure the code compiles and passes tests
"""

        if self.api_type == "openai":
            result = self._call_openai(prompt)
        elif self.api_type == "anthropic":
            result = self._call_anthropic(prompt)
        else:
            print(f"  ✗ Unsupported API type: {self.api_type}")
            return None
        
        # Extract token usage from result if present
        if result and '_token_usage' in result:
            self.last_token_usage = result.pop('_token_usage')
        
        return result
    
    def _call_openai(self, prompt: str) -> Optional[Dict]:
        """Call OpenAI API."""
        try:
            response = requests.post(
                self.api_endpoints["openai"],
                headers={
                    "Authorization": f"Bearer {self.api_key}",
                    "Content-Type": "application/json"
                },
                json={
                    "model": self.model,
                    "messages": [
                        {"role": "system", "content": "You are a code modification assistant. Always respond with valid JSON."},
                        {"role": "user", "content": prompt}
                    ],
                    "temperature": 0.0,
                    "max_tokens": 8000
                },
                timeout=self.api_timeout
            )
            
            if response.status_code != 200:
                print(f"  ✗ API request failed: HTTP {response.status_code}")
                print(f"    Response: {response.text}")
                return None
            
            result = response.json()
            
            # Extract token usage information
            usage = result.get('usage', {})
            self.last_token_usage = {
                'prompt_tokens': usage.get('prompt_tokens', 0),
                'completion_tokens': usage.get('completion_tokens', 0),
                'total_tokens': usage.get('total_tokens', 0)
            }
            
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
                parsed_result = self._parse_llm_response(content)
                
                # Add token usage to the parsed result
                if parsed_result:
                    parsed_result['_token_usage'] = self.last_token_usage
                
                return parsed_result
            
            return None
            
        except requests.exceptions.RequestException as e:
            print(f"  ✗ API request failed: {e}")
            return None
    
    def _call_anthropic(self, prompt: str) -> Optional[Dict]:
        """Call Anthropic API."""
        try:
            response = requests.post(
                self.api_endpoints["anthropic"],
                headers={
                    "x-api-key": self.api_key,
                    "anthropic-version": "2023-06-01",
                    "Content-Type": "application/json"
                },
                json={
                    "model": self.model,
                    "messages": [
                        {"role": "user", "content": prompt}
                    ],
                    "max_tokens": 8000,
                    "temperature": 0.0
                },
                timeout=self.api_timeout
            )
            
            if response.status_code != 200:
                print(f"  ✗ API request failed: HTTP {response.status_code}")
                print(f"    Response: {response.text}")
                return None
            
            result = response.json()
            
            # Extract token usage information for Anthropic
            usage = result.get('usage', {})
            self.last_token_usage = {
                'prompt_tokens': usage.get('input_tokens', 0),
                'completion_tokens': usage.get('output_tokens', 0),
                'total_tokens': usage.get('input_tokens', 0) + usage.get('output_tokens', 0)
            }
            
            if 'content' in result and len(result['content']) > 0:
                content = result['content'][0]['text']
                self.last_raw_response = content
                parsed_result = self._parse_llm_response(content)
                
                # Add token usage to the parsed result
                if parsed_result:
                    parsed_result['_token_usage'] = self.last_token_usage
                
                return parsed_result
            
            return None
            
        except requests.exceptions.RequestException as e:
            print(f"  ✗ API request failed: {e}")
            return None
    
    def _parse_llm_response(self, content: str) -> Optional[Dict]:
        """Parse LLM response to extract JSON."""
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

        print("  ⚠️  Failed to parse JSON response from LLM")
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
    
    def _format_raw_response(self, raw_response: str) -> str:
        """Format raw LLM response for better readability."""
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
                result = f"=== LLM Raw Response (Formatted JSON) ===\n"
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
            result = f"=== LLM Raw Response (JSON Parse Failed) ===\n"
            result += f"Generated at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n"
            result += f"Original length: {len(raw_response)} chars\n"
            result += f"Cleaned length: {len(cleaned)} chars\n"
            result += f"Non-ASCII chars removed: {len(raw_response) - len(cleaned)}\n"
            result += f"JSON Parse Error: {e}\n"
            result += "=" * 50 + "\n\n"
            result += cleaned
            return result
        
        # If no JSON detected, just clean and return with header
        result = f"=== LLM Raw Response (No JSON Detected) ===\n"
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
        Supports common Python test patterns:
        - test_<name>.py (same directory)
        - <name>_test.py (same directory)
        - tests/test_<name>.py (subdirectory)
        - test/test_<name>.py (subdirectory)
        """
        file_name = source_file.stem  # filename without extension
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
            # Find corresponding test file
            test_file = self.find_python_test_file(file_path)
            if test_file:
                return {
                    'language': 'python',
                    'target': str(test_file.relative_to(self.repo_path)),
                    'type': 'file'
                }
            else:
                # If no test file found, at least do syntax check
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
                # Syntax check only
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
                # Run pytest
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
                    # pytest not installed, fallback to syntax check
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
        
        diff_file = output_dir / f"baseline_issue_{issue_num:03d}.diff"
        with open(diff_file, 'w', encoding='utf-8') as f:
            f.write("\n".join(diffs))
        
        print(f"  💾 Diff saved to: {diff_file}")
    
    def save_test_output(self, issue_num: int, test_results: Dict, output_dir: Path):
        """Save test output to a file."""
        if not test_results.get('output'):
            return
        
        test_file = output_dir / f"baseline_issue_{issue_num:03d}_tests.txt"
        with open(test_file, 'w', encoding='utf-8') as f:
            f.write(test_results['output'])
        
        print(f"  💾 Test output saved to: {test_file}")
    
    def process_issue(self, issue_num: int, issue_config: Dict, output_dir: Path) -> Dict:
        """Process a single issue: call LLM, apply changes, run tests, revert."""
        issue = issue_config['issue']
        
        # Support both 'files' and 'folder_path' configurations
        if 'folder_path' in issue_config:
            # Scan folder for .go and .py files
            folder_path = issue_config['folder_path']
            extensions = issue_config.get('extensions', ['.go', '.py'])
            print(f"  📂 Scanning folder: {folder_path} for files with extensions: {extensions}")
            file_paths = self.scan_folder_for_files(folder_path, extensions)
            if not file_paths:
                print(f"  ⚠️  No files found in folder: {folder_path}")
        elif 'files' in issue_config:
            # Use manually specified files
            file_paths = issue_config['files']
        else:
            print(f"  ❌ Issue config must contain either 'files' or 'folder_path'")
            return {
                "issue_num": issue_num,
                "issue": issue,
                "context_files": [],
                "status": "config_error",
                "modified_files": [],
                "test_results": {},
                "token_usage": {},
                "error": "Missing 'files' or 'folder_path' in config"
            }
        
        print(f"\n{'='*80}")
        print(f"📝 Baseline Issue #{issue_num}: {issue[:60]}{'...' if len(issue) > 60 else ''}")
        print(f"{'='*80}")
        
        result = {
            "issue_num": issue_num,
            "issue": issue,
            "context_files": file_paths,
            "status": "pending",
            "modified_files": [],
            "test_results": {},
            "token_usage": {},
            "error": None
        }
        
        # Read file contents
        print(f"  📂 Reading {len(file_paths)} context files...")
        file_contents = self.read_file_contents(file_paths)
        if not file_contents:
            result["status"] = "no_context"
            result["error"] = "Failed to read context files"
            return result
        
        # Call LLM
        modifications = self.call_llm(issue, file_contents)
        
        # Record token usage if available
        if self.last_token_usage:
            result["token_usage"] = self.last_token_usage.copy()
            print(f"  📊 Token usage: {self.last_token_usage['total_tokens']} total "
                  f"({self.last_token_usage['prompt_tokens']} prompt + "
                  f"{self.last_token_usage['completion_tokens']} completion)")
        
        if not modifications:
            result["status"] = "llm_failed"
            result["error"] = "Failed to get modifications from LLM"
            # Save raw response if available
            if self.last_raw_response:
                raw_file = output_dir / f"baseline_issue_{issue_num:03d}_raw.txt"
                try:
                    # Format the raw response nicely
                    formatted_response = self._format_raw_response(self.last_raw_response)
                    with open(raw_file, 'w', encoding='utf-8') as rf:
                        rf.write(formatted_response)
                    print(f"  💾 Saved formatted raw LLM response to {raw_file}")
                except Exception as e:
                    print(f"  ⚠️  Could not save raw response: {e}")
            return result
        
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
    
    def run(self, config_file: str, output_dir: str = "./baseline_outputs"):
        """Main execution: process all issues."""
        print("="*80)
        print("🚀 Baseline Issue Resolution (Direct LLM, No RAG)")
        print("="*80)
        
        # Create output directory
        output_path = Path(output_dir)
        output_path.mkdir(exist_ok=True, parents=True)
        print(f"📁 Output directory: {output_path.resolve()}\n")
        
        # Read issues config
        issues_config = self.read_issues_config(config_file)
        if not issues_config:
            print("❌ No issues to process")
            return
        
        # Process each issue
        for idx, issue_config in enumerate(issues_config, 1):
            result = self.process_issue(idx, issue_config, output_path)
            self.results.append(result)
        
        # Generate summary report
        self.generate_summary_report(output_path)
    
    def generate_summary_report(self, output_dir: Path):
        """Generate a summary report of all issue resolutions."""
        print(f"\n{'='*80}")
        print("📊 BASELINE SUMMARY REPORT")
        print(f"{'='*80}\n")
        
        total = len(self.results)
        passed = sum(1 for r in self.results if r["status"] == "passed")
        failed = sum(1 for r in self.results if r["status"] == "failed")
        llm_failed = sum(1 for r in self.results if r["status"] == "llm_failed")
        no_changes = sum(1 for r in self.results if r["status"] == "no_changes")
        
        print(f"Total Issues:        {total}")
        print(f"Tests Passed:        {passed} ({passed/total*100:.1f}%)")
        print(f"Tests Failed:        {failed} ({failed/total*100:.1f}%)")
        print(f"LLM Failed:          {llm_failed}")
        print(f"No Changes:          {no_changes}")
        print()
        
        # Detailed results
        print("Detailed Results:")
        print("-" * 80)
        for result in self.results:
            status_emoji = {
                "passed": "✅",
                "failed": "❌",
                "llm_failed": "🔴",
                "no_changes": "⚠️"
            }.get(result["status"], "❓")
            
            print(f"{status_emoji} Issue #{result['issue_num']}: {result['issue'][:50]}...")
            print(f"   Context files: {len(result['context_files'])} files")
            if result.get("test_results", {}).get("packages"):
                print(f"   Tested packages: {', '.join(result['test_results']['packages'])}")
            if result.get("error"):
                print(f"   Error: {result['error']}")
            print()
        
        # Save JSON report
        report_file = output_dir / "baseline_summary_report.json"
        with open(report_file, 'w', encoding='utf-8') as f:
            json.dump(self.results, f, indent=2)
        
        print(f"💾 Full report saved to: {report_file}")
        print("="*80)


def main():
    parser = argparse.ArgumentParser(
        description='Baseline issue resolution using direct LLM (no RAG)',
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
        '--config',
        required=True,
        help='Path to JSON config file with issues and file contexts'
    )
    
    parser.add_argument(
        '--api-key',
        default=os.getenv('LLM_API_KEY'),
        help='LLM API key (or set LLM_API_KEY env variable)'
    )
    
    parser.add_argument(
        '--api-type',
        default='openai',
        choices=['openai', 'anthropic'],
        help='LLM API type (default: openai)'
    )
    
    parser.add_argument(
        '--model',
        default='gpt-4',
        help='Model name (default: gpt-4 for OpenAI, claude-3-opus-20240229 for Anthropic)'
    )

    parser.add_argument(
        '--api-url',
        default=None,
        help='Override API endpoint URL (useful for self-hosted / proxy endpoints). Provide the full chat completion/messages URL.'
    )
    
    parser.add_argument(
        '--repo',
        default='.',
        help='Repository path (default: current directory)'
    )
    
    parser.add_argument(
        '--output',
        default='./baseline_outputs',
        help='Output directory (default: ./baseline_outputs)'
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
        default=300,
        help='HTTP timeout (seconds) for LLM API requests (default: 300)'
    )
    
    args = parser.parse_args()
    
    # Validate API key
    if not args.api_key:
        print("❌ Error: API key is required. Use --api-key or set LLM_API_KEY environment variable")
        sys.exit(1)
    
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
    resolver = BaselineResolver(
        args.repo,
        args.api_key,
        args.api_type,
        args.model,
        api_url=args.api_url,
        head_lines=args.head_lines,
        api_timeout=args.api_timeout,
    )
    resolver.run(args.config, args.output)


if __name__ == "__main__":
    main()