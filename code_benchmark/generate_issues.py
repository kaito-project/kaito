#!/usr/bin/env python3
"""
Intelligent Issue Generator for Code Benchmark.
Uses code repository structure analysis to generate realistic issues.
"""

import os
import sys
import json
import argparse
import requests
from pathlib import Path
from typing import List, Dict, Set, Optional
from collections import Counter, defaultdict


class CodebaseAnalyzer:
    """Analyze codebase structure to understand components."""
    
    def __init__(self, repo_path: str, llm_url: str = None, model: str = "deepseek-v3.1", index_name: str = "kaito_code_benchmark"):
        self.repo_path = Path(repo_path).resolve()
        self.go_files = []
        self.py_files = []
        self.structure = defaultdict(list)
        self.llm_url = llm_url
        self.model = model
        self.index_name = index_name
        
    def scan_repository(self):
        """Scan repository to build structure map."""
        print("📁 Scanning repository structure...")
        
        ignored_dirs = {
            '.git', '__pycache__', 'node_modules', 'vendor',
            '.venv', 'venv', 'dist', 'build', '.idea', '.vscode'
        }
        
        go_count = 0
        py_count = 0
        
        for root, dirs, files in os.walk(self.repo_path):
            # Filter ignored directories
            dirs[:] = [d for d in dirs if d not in ignored_dirs]
            
            root_path = Path(root)
            rel_path = root_path.relative_to(self.repo_path)
            
            for file in files:
                file_path = root_path / file
                rel_file_path = file_path.relative_to(self.repo_path)
                
                if file.endswith('.go'):
                    self.go_files.append(str(rel_file_path))
                    self.structure[str(rel_path)].append(file)
                    go_count += 1
                elif file.endswith('.py'):
                    self.py_files.append(str(rel_file_path))
                    py_count += 1
        
        print(f"  ✓ Found {go_count} Go files")
        print(f"  ✓ Found {py_count} Python files")
        print(f"  ✓ Scanned {len(self.structure)} directories")
        
        return self.structure
    
    def identify_components(self):
        """Identify main components from directory structure."""
        print("\n🔍 Identifying code components...")
        
        components = {}
        
        # Analyze Go code structure
        for dir_path, files in self.structure.items():
            if not files:
                continue
                
            # Skip root and test-only directories
            if dir_path == '.':
                continue
            
            parts = Path(dir_path).parts
            
            # Identify component type based on path patterns
            component_info = {
                'path': dir_path,
                'files': files,
                'file_count': len(files),
                'type': self._identify_component_type(dir_path, files)
            }
            
            if component_info['type'] != 'other':
                components[dir_path] = component_info
        
        # Print summary
        type_counts = Counter(c['type'] for c in components.values())
        print(f"  Component types found:")
        for comp_type, count in type_counts.most_common():
            print(f"    - {comp_type}: {count} components")
        
        return components
    
    def _identify_component_type(self, dir_path: str, files: List[str]) -> str:
        """Identify what type of component this directory contains."""
        path_lower = dir_path.lower()
        
        # Controller/Reconciler
        if 'controller' in path_lower or 'reconcil' in path_lower:
            return 'controller'
        
        # API definitions
        if 'api' in path_lower or path_lower.startswith('api/'):
            return 'api'
        
        # Business logic packages
        if path_lower.startswith('pkg/'):
            if 'workspace' in path_lower:
                return 'workspace_pkg'
            elif 'sku' in path_lower:
                return 'sku_pkg'
            elif 'estimator' in path_lower:
                return 'estimator'
            else:
                return 'pkg'
        
        # Tests
        if 'test' in path_lower or any('_test.go' in f for f in files):
            return 'test'
        
        # Config
        if 'config' in path_lower or 'cmd' in path_lower:
            return 'config'
        
        return 'other'
    
    def extract_code_patterns(self) -> Dict[str, List[str]]:
        """Extract code patterns by reading file contents."""
        print("\n🔍 Analyzing code patterns...")
        
        patterns = {
            'functions': [],
            'types': [],
            'structs': [],
            'interfaces': [],
            'constants': []
        }
        
        # Sample some files to find patterns
        import re
        sample_files = self.go_files[:30]  # Analyze first 30 files
        
        for file_path in sample_files:
            try:
                full_path = self.repo_path / file_path
                with open(full_path, 'r', encoding='utf-8') as f:
                    content = f.read()
                
                # Extract function names
                func_matches = re.findall(r'func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(', content)
                patterns['functions'].extend(func_matches[:5])  # First 5 functions per file
                
                # Extract type names
                type_matches = re.findall(r'type\s+(\w+)\s+(?:struct|interface)', content)
                patterns['types'].extend(type_matches)
                
                # Extract struct names
                struct_matches = re.findall(r'type\s+(\w+)\s+struct', content)
                patterns['structs'].extend(struct_matches)
                
            except Exception as e:
                continue
        
        # Remove duplicates and get most common
        for key in patterns:
            patterns[key] = list(set(patterns[key]))[:10]  # Top 10 unique items
        
        print(f"  Found {len(patterns['functions'])} function patterns")
        print(f"  Found {len(patterns['types'])} type patterns")
        
        return patterns
    
    def generate_codebase_summary(self) -> str:
        """Generate a summary of the codebase structure."""
        components = self.identify_components()
        
        summary_lines = ["Repository Structure Summary:", ""]
        
        # Group by type
        by_type = defaultdict(list)
        for comp in components.values():
            by_type[comp['type']].append(comp)
        
        for comp_type, comps in sorted(by_type.items()):
            summary_lines.append(f"- {comp_type}: {len(comps)} directories")
            for comp in comps[:3]:  # Show first 3 examples
                summary_lines.append(f"  * {comp['path']} ({comp['file_count']} files)")
            if len(comps) > 3:
                summary_lines.append(f"  * ... and {len(comps) - 3} more")
        
        summary_lines.append(f"\nTotal Go files: {len(self.go_files)}")
        summary_lines.append(f"Total Python files: {len(self.py_files)}")
        
        return "\n".join(summary_lines)
    
    def suggest_issues(self, count: int) -> List[Dict]:
        """Use LLM to generate completely random, realistic issues."""
        print(f"\n🤖 Using LLM to generate {count} completely random issues...")
        
        if not self.llm_url:
            raise ValueError("LLM URL is required. Please provide --llm-url parameter.")
        
        # Get codebase summary
        codebase_summary = self.generate_codebase_summary()
        components = self.identify_components()
        
        # Build component list for LLM
        component_dirs = list(components.keys())[:15]  # First 15 directories
        
        # Simplified, more direct prompt for faster generation
        prompt = f"""Generate {count} realistic code modification tasks for this codebase:

{codebase_summary}

Available directories: {', '.join(component_dirs[:10])}

Each task must be:
- SPECIFIC (mention exact changes needed, not vague goals)
- ACTIONABLE (a developer knows exactly what to implement)
- REALISTIC (actual work a developer would do in this codebase)
- DIVERSE (cover different aspects: features, fixes, improvements, etc.)

Output ONLY valid JSON array (no markdown, no explanation):
[
  {{"description": "specific task with details", "target_dirs": ["dir1"], "keywords": ["key1", "key2"]}},
  {{"description": "another specific task", "target_dirs": ["dir2"], "keywords": ["key3"]}}
]

JSON:"""

        try:
            print(f"  🌐 Calling LLM (max 2 minutes)...")
            response = requests.post(
                f"{self.llm_url}/v1/chat/completions",
                json={
                    "index_name": self.index_name,
                    "model": self.model,
                    "messages": [
                        {"role": "system", "content": "You are a code task generator. Output only valid JSON, no markdown."},
                        {"role": "user", "content": prompt}
                    ],
                    "max_tokens": 1200,
                    "temperature": 0.8,
                    "stream": False
                },
                timeout=120
            )
            
            if response.status_code != 200:
                raise Exception(f"HTTP {response.status_code}: {response.text[:200]}")
            
            data = response.json()
            result = data['choices'][0]['message']['content']
            
            print(f"  📝 LLM response received ({len(result)} chars)")
            
            # Extract JSON from response (handle markdown code blocks)
            import re
            # Try to find JSON array
            json_match = re.search(r'\[[\s\S]*\]', result)
            if not json_match:
                raise Exception("No JSON array found in LLM response")
            
            json_str = json_match.group()
            issues = json.loads(json_str)
            
            if not isinstance(issues, list):
                raise Exception("LLM output is not a list")
            
            print(f"  ✓ Successfully parsed {len(issues)} issues from LLM")
            
            # Validate and fix issues
            valid_issues = []
            for idx, issue in enumerate(issues):
                if not isinstance(issue, dict):
                    continue
                
                # Ensure required fields
                if 'description' not in issue:
                    continue
                
                # Fix target_dirs if missing or invalid
                if 'target_dirs' not in issue or not issue['target_dirs']:
                    # Try to match from keywords
                    keywords = issue.get('keywords', [])
                    matched = [d for d in component_dirs if any(kw.lower() in d.lower() for kw in keywords)]
                    issue['target_dirs'] = matched if matched else [component_dirs[0]]
                
                # Clean target_dirs: if it contains a file path, extract just the directory
                cleaned_dirs = []
                for target_dir in issue.get('target_dirs', []):
                    # If last part contains a dot (likely a filename), remove it
                    parts = target_dir.split('/')
                    if parts and '.' in parts[-1]:
                        # Remove the filename part
                        target_dir = '/'.join(parts[:-1])
                    if target_dir:  # Only add non-empty paths
                        cleaned_dirs.append(target_dir)
                issue['target_dirs'] = cleaned_dirs if cleaned_dirs else [component_dirs[0]]
                
                # Ensure keywords exist
                if 'keywords' not in issue or not issue['keywords']:
                    # Extract keywords from description
                    words = issue['description'].split()
                    issue['keywords'] = [w.strip('.,;:') for w in words if len(w) > 4][:3]
                
                valid_issues.append(issue)
            
            if len(valid_issues) < count:
                print(f"  ⚠️  Only {len(valid_issues)} valid issues (requested {count})")
            
            return valid_issues[:count]
                
        except Exception as e:
            print(f"\n❌ LLM generation failed: {e}")
            print(f"   Please ensure LLM service is running at {self.llm_url}")
            raise


class IssueGenerator:
    def __init__(
        self,
        repo_path: str,
        llm_url: str = None,
        model: str = "deepseek-v3.1",
        index_name: str = "kaito_code_benchmark"
    ):
        self.repo_path = Path(repo_path).resolve()
        self.llm_url = llm_url
        self.model = model
        self.index_name = index_name
        self.analyzer = CodebaseAnalyzer(repo_path, llm_url=llm_url, model=model, index_name=index_name)
        
    def generate_issues(self, templates: List[Dict]) -> tuple[List[Dict], List[str]]:
        """Generate issues from templates with folder_path determination."""
        print(f"\n{'='*80}")
        print(f"🚀 Issue Generation Starting")
        print(f"{'='*80}")
        
        baseline_config = []
        rag_issues = []
        
        for template in templates:
            print(f"\n{'='*80}")
            print(f"📝 Generating issue: {template['description'][:60]}...")
            print(f"{'='*80}")
            
            # Determine folder path from target directories or keywords
            folder_path = self._determine_folder_path(template)
            
            if folder_path:
                # Baseline format (with folder_path)
                baseline_issue = {
                    "issue": template['description'],
                    "folder_path": folder_path,
                    "extensions": [".go"]
                }
                baseline_config.append(baseline_issue)
                
                # RAG format (plain text)
                rag_issues.append(template['description'])
                
                print(f"  ✓ Determined folder_path: {folder_path}")
            else:
                print(f"  ⚠️  Could not determine folder_path, skipping")
        
        return baseline_config, rag_issues
    
    def _determine_folder_path(self, template: Dict) -> Optional[str]:
        """Determine folder path from template."""
        # Use target_dirs if available
        if 'target_dirs' in template and template['target_dirs']:
            # Use the first target directory
            return template['target_dirs'][0]
        
        # Fallback: search by keywords
        keywords = template.get('keywords', [])
        if not keywords:
            return None
        
        # Search for directories matching keywords
        for dir_path in self.analyzer.structure.keys():
            dir_lower = dir_path.lower()
            if any(kw.lower() in dir_lower for kw in keywords):
                return dir_path
        
        return None
    
    def save_configs(
        self,
        baseline_config: List[Dict],
        rag_issues: List[str],
        baseline_file: str = "issues_baseline_generated.json",
        rag_file: str = "issues_generated.txt"
    ):
        """Save generated configurations to files."""
        print(f"\n{'='*80}")
        print(f"💾 Saving Generated Issues")
        print(f"{'='*80}")
        
        # Save baseline config
        baseline_path = self.repo_path / baseline_file
        with open(baseline_path, 'w', encoding='utf-8') as f:
            json.dump(baseline_config, f, indent=2)
        
        print(f"✅ Baseline config saved to: {baseline_path}")
        print(f"   {len(baseline_config)} issues with folder_path")
        
        # Save RAG issues
        rag_path = self.repo_path / rag_file
        with open(rag_path, 'w', encoding='utf-8') as f:
            for issue in rag_issues:
                f.write(issue + '\n')
        
        print(f"✅ RAG issues saved to: {rag_path}")
        print(f"   {len(rag_issues)} issues (plain text)")
        
        print(f"\n{'='*80}")
        print(f"🎉 Issue Generation Complete!")
        print(f"{'='*80}")


def main():
    parser = argparse.ArgumentParser(
        description='Generate issues for code benchmark based on repository structure',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Generate 5 issues by analyzing codebase structure
  python generate_issues.py --repo . --count 5

  # Use custom templates
  python generate_issues.py --repo . --templates issue_templates.json
        """
    )
    
    parser.add_argument(
        '--repo',
        default='.',
        help='Repository path (default: current directory)'
    )
    
    parser.add_argument(
        '--count',
        type=int,
        default=5,
        help='Number of issues to generate (default: 5)'
    )
    
    parser.add_argument(
        '--llm-url',
        required=True,
        help='LLM service URL for issue generation (e.g., http://localhost:5000)'
    )
    
    parser.add_argument(
        '--model',
        default='deepseek-v3.1',
        help='Model name for LLM (default: deepseek-v3.1)'
    )
    
    parser.add_argument(
        '--index',
        default='kaito_code_benchmark',
        help='RAG index name (default: kaito_code_benchmark)'
    )
    
    parser.add_argument(
        '--templates',
        help='JSON file with issue templates (optional)'
    )
    
    parser.add_argument(
        '--baseline-output',
        default='issues_baseline_generated.json',
        help='Output file for baseline config (default: issues_baseline_generated.json)'
    )
    
    parser.add_argument(
        '--rag-output',
        default='issues_generated.txt',
        help='Output file for RAG issues (default: issues_generated.txt)'
    )
    
    args = parser.parse_args()
    
    # Create generator
    generator = IssueGenerator(
        repo_path=args.repo,
        llm_url=args.llm_url,
        model=args.model,
        index_name=args.index
    )
    
    # Scan repository first
    generator.analyzer.scan_repository()
    
    # Load or generate templates
    if args.templates:
        with open(args.templates, 'r', encoding='utf-8') as f:
            templates = json.load(f)
        print(f"📋 Loaded {len(templates)} templates from {args.templates}")
    else:
        # Generate templates using LLM
        templates = generator.analyzer.suggest_issues(args.count)
        print(f"📋 Generated {len(templates)} issue templates")
    
    # Generate issues
    baseline_config, rag_issues = generator.generate_issues(templates)
    
    # Save configurations
    generator.save_configs(
        baseline_config,
        rag_issues,
        baseline_file=args.baseline_output,
        rag_file=args.rag_output
    )


if __name__ == "__main__":
    main()
