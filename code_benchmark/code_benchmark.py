#!/usr/bin/env python3
"""
Code Issue Resolution Benchmark: RAG vs Baseline (Pure LLM)
Runs both approaches and generates comparison report.
"""

import os
import sys
import json
import argparse
import subprocess
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Optional


class CodeBenchmark:
    def __init__(
        self,
        repo_path: str,
        issues_file: str,
        baseline_config: str,
        rag_url: str,
        rag_index: str,
        llm_api_key: str,
        llm_api_url: str,
        model: str = "deepseek-v3.1",
        output_dir: str = "./code_benchmark_outputs"
    ):
        """
        Initialize code benchmark.
        
        Args:
            repo_path: Path to repository
            issues_file: Issues file for RAG (one per line)
            baseline_config: Config JSON for baseline (with files specification)
            rag_url: RAG service URL
            rag_index: RAG index name
            llm_api_key: API key for baseline LLM
            llm_api_url: API URL for baseline LLM
            model: Model name
            output_dir: Output directory
        """
        self.repo_path = Path(repo_path).resolve()
        self.issues_file = issues_file
        self.baseline_config = baseline_config
        self.rag_url = rag_url
        self.rag_index = rag_index
        self.llm_api_key = llm_api_key
        self.llm_api_url = llm_api_url
        self.model = model
        self.output_dir = Path(output_dir)
        
        # Output subdirectories
        self.baseline_dir = self.output_dir / "baseline_outputs"
        self.rag_dir = self.output_dir / "rag_outputs"
        
    def run_baseline(self) -> bool:
        """Run baseline (pure LLM) resolver."""
        print("="*80)
        print("🔵 PHASE 1: Running Baseline (Pure LLM)")
        print("="*80)
        
        # Find script in repo path
        script_path = self.repo_path / "resolve_issues_baseline.py"
        
        cmd = [
            sys.executable,
            str(script_path),
            "--config", self.baseline_config,
            "--api-key", self.llm_api_key,
            "--api-type", "openai",
            "--model", self.model,
            "--api-url", self.llm_api_url,
            "--repo", str(self.repo_path),
            "--output", str(self.baseline_dir)
        ]
        
        print(f"📝 Command: {' '.join(cmd)}")
        print()
        
        try:
            result = subprocess.run(
                cmd,
                cwd=str(self.repo_path),
                check=True,
                capture_output=False  # Show output in real-time
            )
            print("\n✅ Baseline completed successfully\n")
            return True
        except subprocess.CalledProcessError as e:
            print(f"\n❌ Baseline failed with exit code {e.returncode}\n")
            return False
    
    def run_rag(self) -> bool:
        """Run RAG-enhanced resolver."""
        print("="*80)
        print("🟢 PHASE 2: Running RAG-Enhanced")
        print("="*80)
        
        # Find script in repo path
        script_path = self.repo_path / "rag_solution.py"
        
        cmd = [
            sys.executable,
            str(script_path),
            "--issues", self.issues_file,
            "--url", self.rag_url,
            "--index", self.rag_index,
            "--model", self.model,
            "--repo", str(self.repo_path),
            "--output", str(self.rag_dir)
        ]
        
        print(f"📝 Command: {' '.join(cmd)}")
        print()
        
        try:
            result = subprocess.run(
                cmd,
                cwd=str(self.repo_path),
                check=True,
                capture_output=False  # Show output in real-time
            )
            print("\n✅ RAG completed successfully\n")
            return True
        except subprocess.CalledProcessError as e:
            print(f"\n❌ RAG failed with exit code {e.returncode}\n")
            return False
    
    def load_results(self) -> tuple[Optional[Dict], Optional[Dict]]:
        """Load results from both runs."""
        baseline_report = self.baseline_dir / "baseline_summary_report.json"
        rag_report = self.rag_dir / "rag_summary_report.json"
        
        baseline_data = None
        rag_data = None
        
        if baseline_report.exists():
            with open(baseline_report, 'r', encoding='utf-8') as f:
                baseline_data = json.load(f)
            print(f"✅ Loaded baseline results from {baseline_report}")
        else:
            print(f"⚠️  Baseline report not found: {baseline_report}")
        
        if rag_report.exists():
            with open(rag_report, 'r', encoding='utf-8') as f:
                rag_data = json.load(f)
            print(f"✅ Loaded RAG results from {rag_report}")
        else:
            print(f"⚠️  RAG report not found: {rag_report}")
        
        return baseline_data, rag_data
    
    def compare_results(self, baseline_data: Dict, rag_data: Dict) -> Dict:
        """Compare results from both approaches."""
        print("\n" + "="*80)
        print("📊 PHASE 3: Comparing Results")
        print("="*80 + "\n")
        
        # Extract summary stats
        baseline_summary = self._extract_summary(baseline_data, "Baseline")
        rag_summary = self._extract_summary(rag_data, "RAG")
        
        # Calculate improvements
        comparison = {
            "baseline": baseline_summary,
            "rag": rag_summary,
            "improvements": self._calculate_improvements(baseline_summary, rag_summary)
        }
        
        return comparison
    
    def _extract_summary(self, data: Dict, label: str) -> Dict:
        """Extract summary statistics from results."""
        # Handle different JSON structures
        if "summary" in data:
            # RAG format with summary section
            summary = data["summary"]
            issues = data.get("issues", [])
            
            total = summary.get("total_issues", 0)
            passed = summary.get("tests_passed", 0)
            failed = summary.get("tests_failed", 0)
            
            tokens_usage = summary.get("tokens_usage", {})
            total_tokens = tokens_usage.get("total_tokens", 0)
            prompt_tokens = tokens_usage.get("total_prompt_tokens", 0)
            completion_tokens = tokens_usage.get("total_completion_tokens", 0)
        else:
            # Baseline format with flat list
            issues = data if isinstance(data, list) else []
            
            total = len(issues)
            passed = sum(1 for r in issues if r.get("status") == "passed")
            failed = sum(1 for r in issues if r.get("status") == "failed")
            
            # Calculate token usage from individual issues
            total_tokens = 0
            prompt_tokens = 0
            completion_tokens = 0
            
            for issue in issues:
                usage = issue.get("token_usage", {})
                total_tokens += usage.get("total_tokens", 0)
                prompt_tokens += usage.get("prompt_tokens", 0)
                completion_tokens += usage.get("completion_tokens", 0)
        
        success_rate = (passed / total * 100) if total > 0 else 0
        avg_tokens = (total_tokens / total) if total > 0 else 0
        
        summary = {
            "label": label,
            "total_issues": total,
            "tests_passed": passed,
            "tests_failed": failed,
            "success_rate": success_rate,
            "total_tokens": total_tokens,
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "avg_tokens_per_issue": avg_tokens
        }
        
        print(f"📋 {label} Summary:")
        print(f"   Total Issues:  {total}")
        print(f"   Tests Passed:  {passed} ({success_rate:.1f}%)")
        print(f"   Tests Failed:  {failed}")
        print(f"   Total Tokens:  {total_tokens:,}")
        print(f"   Avg per Issue: {avg_tokens:.1f} tokens")
        print()
        
        return summary
    
    def _calculate_improvements(self, baseline: Dict, rag: Dict) -> Dict:
        """Calculate improvement metrics."""
        improvements = {}
        
        # Success rate improvement
        baseline_rate = baseline["success_rate"]
        rag_rate = rag["success_rate"]
        
        if baseline_rate > 0:
            rate_improvement = ((rag_rate - baseline_rate) / baseline_rate) * 100
            rate_diff = rag_rate - baseline_rate
        else:
            rate_improvement = float('inf') if rag_rate > 0 else 0
            rate_diff = rag_rate
        
        improvements["success_rate_improvement"] = rate_improvement
        improvements["success_rate_diff"] = rate_diff
        
        # Token efficiency
        baseline_tokens = baseline["total_tokens"]
        rag_tokens = rag["total_tokens"]
        
        if baseline_tokens > 0:
            token_efficiency = ((baseline_tokens - rag_tokens) / baseline_tokens) * 100
            token_ratio = rag_tokens / baseline_tokens
        else:
            token_efficiency = 0
            token_ratio = 0
        
        improvements["token_efficiency"] = token_efficiency
        improvements["token_ratio"] = token_ratio
        
        # Tests improvement
        baseline_passed = baseline["tests_passed"]
        rag_passed = rag["tests_passed"]
        tests_improvement = rag_passed - baseline_passed
        
        improvements["tests_improvement"] = tests_improvement
        
        print("📈 Improvements (RAG vs Baseline):")
        print(f"   Success Rate: {rag_rate:.1f}% vs {baseline_rate:.1f}% ({rate_diff:+.1f}pp, {rate_improvement:+.1f}%)")
        print(f"   Tests Passed: {rag_passed} vs {baseline_passed} ({tests_improvement:+d})")
        print(f"   Token Usage:  {rag_tokens:,} vs {baseline_tokens:,} ({token_efficiency:+.1f}% efficiency)")
        print(f"   Token Ratio:  {token_ratio:.2f}x")
        print()
        
        return improvements
    
    def generate_comparison_report(self, comparison: Dict):
        """Generate comprehensive comparison report."""
        print("="*80)
        print("📄 Generating Comparison Report")
        print("="*80 + "\n")
        
        report = {
            "timestamp": datetime.now().isoformat(),
            "model": self.model,
            "repository": str(self.repo_path),
            "comparison": comparison
        }
        
        # Save JSON report
        report_file = self.output_dir / "code_benchmark_comparison.json"
        with open(report_file, 'w', encoding='utf-8') as f:
            json.dump(report, f, indent=2)
        
        print(f"💾 JSON report saved: {report_file}")
        
        # Generate human-readable summary
        summary_file = self.output_dir / "code_benchmark_summary.txt"
        with open(summary_file, 'w', encoding='utf-8') as f:
            f.write("="*80 + "\n")
            f.write("CODE BENCHMARK COMPARISON REPORT\n")
            f.write("RAG-Enhanced vs Baseline (Pure LLM)\n")
            f.write("="*80 + "\n\n")
            
            f.write(f"Timestamp: {report['timestamp']}\n")
            f.write(f"Model: {self.model}\n")
            f.write(f"Repository: {self.repo_path}\n\n")
            
            baseline = comparison["baseline"]
            rag = comparison["rag"]
            improvements = comparison["improvements"]
            
            f.write("-"*80 + "\n")
            f.write("BASELINE (Pure LLM)\n")
            f.write("-"*80 + "\n")
            f.write(f"Total Issues:        {baseline['total_issues']}\n")
            f.write(f"Tests Passed:        {baseline['tests_passed']} ({baseline['success_rate']:.1f}%)\n")
            f.write(f"Tests Failed:        {baseline['tests_failed']}\n")
            f.write(f"Total Tokens:        {baseline['total_tokens']:,}\n")
            f.write(f"Prompt Tokens:       {baseline['prompt_tokens']:,}\n")
            f.write(f"Completion Tokens:   {baseline['completion_tokens']:,}\n")
            f.write(f"Avg Tokens/Issue:    {baseline['avg_tokens_per_issue']:.1f}\n\n")
            
            f.write("-"*80 + "\n")
            f.write("RAG-ENHANCED\n")
            f.write("-"*80 + "\n")
            f.write(f"Total Issues:        {rag['total_issues']}\n")
            f.write(f"Tests Passed:        {rag['tests_passed']} ({rag['success_rate']:.1f}%)\n")
            f.write(f"Tests Failed:        {rag['tests_failed']}\n")
            f.write(f"Total Tokens:        {rag['total_tokens']:,}\n")
            f.write(f"Prompt Tokens:       {rag['prompt_tokens']:,}\n")
            f.write(f"Completion Tokens:   {rag['completion_tokens']:,}\n")
            f.write(f"Avg Tokens/Issue:    {rag['avg_tokens_per_issue']:.1f}\n\n")
            
            f.write("="*80 + "\n")
            f.write("IMPROVEMENTS (RAG vs Baseline)\n")
            f.write("="*80 + "\n")
            f.write(f"Success Rate:        {improvements['success_rate_diff']:+.1f}pp ({improvements['success_rate_improvement']:+.1f}%)\n")
            f.write(f"Tests Improvement:   {improvements['tests_improvement']:+d}\n")
            f.write(f"Token Efficiency:    {improvements['token_efficiency']:+.1f}%\n")
            f.write(f"Token Ratio:         {improvements['token_ratio']:.2f}x\n")
            
            # Winner determination
            f.write("\n" + "="*80 + "\n")
            f.write("VERDICT\n")
            f.write("="*80 + "\n")
            
            if improvements['success_rate_diff'] > 0:
                f.write(f"🏆 RAG is better by {improvements['success_rate_diff']:.1f}pp in success rate\n")
            elif improvements['success_rate_diff'] < 0:
                f.write(f"🏆 Baseline is better by {abs(improvements['success_rate_diff']):.1f}pp in success rate\n")
            else:
                f.write("🤝 Both approaches have equal success rates\n")
            
            if improvements['token_efficiency'] > 0:
                f.write(f"💰 RAG is {improvements['token_efficiency']:.1f}% more token-efficient\n")
            elif improvements['token_efficiency'] < 0:
                f.write(f"💰 Baseline is {abs(improvements['token_efficiency']):.1f}% more token-efficient\n")
            else:
                f.write("💰 Both approaches use similar tokens\n")
        
        print(f"📄 Summary report saved: {summary_file}\n")
        
        # Print summary to console
        print("="*80)
        print("🎯 FINAL SUMMARY")
        print("="*80)
        print(f"\n✅ Baseline: {baseline['tests_passed']}/{baseline['total_issues']} passed ({baseline['success_rate']:.1f}%)")
        print(f"✅ RAG:      {rag['tests_passed']}/{rag['total_issues']} passed ({rag['success_rate']:.1f}%)")
        print(f"\n💡 RAG Success Improvement: {improvements['success_rate_diff']:+.1f}pp ({improvements['success_rate_improvement']:+.1f}%)")
        print(f"💰 Token Efficiency: {improvements['token_efficiency']:+.1f}%")
        print(f"📊 Token Ratio: {improvements['token_ratio']:.2f}x")
        print("\n" + "="*80)
    
    def run(self):
        """Run complete benchmark pipeline."""
        print("\n" + "="*80)
        print("🚀 CODE ISSUE RESOLUTION BENCHMARK")
        print("RAG-Enhanced vs Baseline (Pure LLM)")
        print("="*80 + "\n")
        
        print(f"📁 Repository:      {self.repo_path}")
        print(f"📝 Issues File:     {self.issues_file}")
        print(f"⚙️  Baseline Config: {self.baseline_config}")
        print(f"🔗 RAG URL:         {self.rag_url}")
        print(f"📚 RAG Index:       {self.rag_index}")
        print(f"🤖 Model:           {self.model}")
        print(f"📂 Output Dir:      {self.output_dir}")
        print()
        
        # Create output directories
        self.output_dir.mkdir(exist_ok=True, parents=True)
        
        # Run baseline
        baseline_success = self.run_baseline()
        
        # Run RAG
        rag_success = self.run_rag()
        
        # Load and compare results
        if baseline_success and rag_success:
            baseline_data, rag_data = self.load_results()
            
            if baseline_data and rag_data:
                comparison = self.compare_results(baseline_data, rag_data)
                self.generate_comparison_report(comparison)
                print("\n✅ Benchmark completed successfully!")
            else:
                print("\n❌ Failed to load results from one or both runs")
                sys.exit(1)
        else:
            print("\n❌ One or both benchmark runs failed")
            sys.exit(1)


def main():
    parser = argparse.ArgumentParser(
        description='Code Issue Resolution Benchmark: RAG vs Baseline',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Run benchmark with all parameters
  python code_benchmark.py \\
    --issues issues.txt \\
    --baseline-config issues_baseline.json \\
    --rag-url http://localhost:5000 \\
    --rag-index code_repo \\
    --llm-api-key sk-xxx \\
    --llm-api-url http://localhost:8081 \\
    --model deepseek-v3.1 \\
    --repo . \\
    --output code_benchmark_outputs

  # Using environment variable for API key
  export LLM_API_KEY=sk-xxx
  python code_benchmark.py \\
    --issues issues.txt \\
    --baseline-config issues_baseline.json \\
    --rag-url http://localhost:5000 \\
    --rag-index code_repo \\
    --llm-api-url http://localhost:8081
        """
    )
    
    parser.add_argument(
        '--issues',
        required=True,
        help='Issues file for RAG (one issue per line)'
    )
    
    parser.add_argument(
        '--baseline-config',
        required=True,
        help='Config JSON for baseline (with files specification)'
    )
    
    parser.add_argument(
        '--rag-url',
        default='http://localhost:5000',
        help='RAG service URL (default: http://localhost:5000)'
    )
    
    parser.add_argument(
        '--rag-index',
        required=True,
        help='RAG index name'
    )
    
    parser.add_argument(
        '--llm-api-key',
        default=os.getenv('LLM_API_KEY'),
        help='LLM API key for baseline (or set LLM_API_KEY env variable)'
    )
    
    parser.add_argument(
        '--llm-api-url',
        required=True,
        help='LLM API URL for baseline'
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
        default='./code_benchmark_outputs',
        help='Output directory (default: ./code_benchmark_outputs)'
    )
    
    args = parser.parse_args()
    
    # Validate API key
    if not args.llm_api_key:
        print("❌ Error: API key is required. Use --llm-api-key or set LLM_API_KEY environment variable")
        sys.exit(1)
    
    # Validate repository
    repo_path = Path(args.repo)
    if not repo_path.is_dir():
        print(f"❌ Error: Repository path does not exist: {args.repo}")
        sys.exit(1)
    
    # Validate issues file
    if not Path(args.issues).exists():
        print(f"❌ Error: Issues file not found: {args.issues}")
        sys.exit(1)
    
    # Validate baseline config
    if not Path(args.baseline_config).exists():
        print(f"❌ Error: Baseline config not found: {args.baseline_config}")
        sys.exit(1)
    
    # Create and run benchmark
    benchmark = CodeBenchmark(
        repo_path=args.repo,
        issues_file=args.issues,
        baseline_config=args.baseline_config,
        rag_url=args.rag_url,
        rag_index=args.rag_index,
        llm_api_key=args.llm_api_key,
        llm_api_url=args.llm_api_url,
        model=args.model,
        output_dir=args.output
    )
    
    benchmark.run()


if __name__ == "__main__":
    main()
