# Code Benchmark: Complete Usage Guide

## Table of Contents

1. [Introduction](#introduction)
2. [System Requirements](#system-requirements)
3. [Installation](#installation)
4. [Component Details](#component-details)
5. [Step-by-Step Tutorial](#step-by-step-tutorial)
6. [Advanced Usage](#advanced-usage)
7. [Best Practices](#best-practices)
8. [Troubleshooting](#troubleshooting)

---

## Introduction

The Code Benchmark suite is designed to objectively compare two approaches for automated code issue resolution:

- **Baseline Approach**: Traditional LLM with manually provided context
- **RAG Approach**: Retrieval-Augmented Generation with automatic context retrieval

This guide provides comprehensive instructions for running benchmarks and interpreting results.

## System Requirements

### Software Requirements

- Python 3.8 or higher
- Git (for diff generation and file management)
- Go compiler (if testing Go repositories)
- Python test frameworks (if testing Python repositories)

### Python Dependencies

```bash
pip install openai anthropic requests pathlib typing
```

### Service Requirements

#### For Baseline Solution
- LLM API access (OpenAI, Anthropic, or compatible endpoint)
- API key with sufficient quota

#### For RAG Solution
- RAG service running on accessible endpoint (default: http://localhost:5000)
- Pre-built vector index of your codebase
- RAG service must support `/v1/chat/completions` endpoint

## Installation

### Clone or Copy Files

```bash
# If part of a larger project
cd /path/to/project

# Create benchmark directory
mkdir code_benchmark
cd code_benchmark

# Copy benchmark files
cp /path/to/generate_issues.py .
cp /path/to/resolve_issues_baseline.py .
cp /path/to/rag_solution.py .
cp /path/to/code_benchmark.py .
```

### Verify Installation

```bash
# Check Python syntax
python3 -m py_compile generate_issues.py
python3 -m py_compile resolve_issues_baseline.py
python3 -m py_compile rag_solution.py
python3 -m py_compile code_benchmark.py

echo "✅ All files validated"
```

## Component Details

### 1. generate_issues.py

**Purpose**: Creates realistic test issues based on repository analysis.

**Key Features**:
- Scans repository structure (Go, Python, etc.)
- Identifies components, packages, and modules
- Generates context-aware issues
- Supports custom templates
- Optional LLM-assisted generation for smarter issues

**Usage Pattern**:
```bash
python generate_issues.py --repo <PATH> --count <N> [OPTIONS]
```

**Full Options**:
```
--repo PATH          Path to repository (required)
--count N            Number of issues to generate (default: 5)
--output FILE        Output file (default: generated_issues.txt)
--llm-url URL        LLM endpoint for smart generation (optional)
--model NAME         Model name (default: deepseek-v3.1)
--api-key KEY        API key if using LLM
--temperature FLOAT  Temperature for LLM (default: 0.7)
```

**Output Format**:
```
Add error handling for nil workspace spec in validation
Fix memory leak in GPU resource cleanup
Update deprecated API usage in model controller
...
```

### 2. resolve_issues_baseline.py

**Purpose**: Resolves issues using direct LLM calls.

**Key Features**:
- Manual context provision (you select which files to include)
- Multiple LLM provider support (OpenAI, Anthropic)
- Automatic test execution
- Git diff generation
- Comprehensive error reporting

**Usage Pattern**:
```bash
python resolve_issues_baseline.py \
  --repo <PATH> \
  --issues <FILE> \
  --output <DIR> \
  --api-key <KEY>
```

**Full Options**:
```
--repo PATH          Repository path (required)
--issues FILE        Issues file (required)
--output DIR         Output directory (default: baseline_outputs)
--api-key KEY        LLM API key (required)
--model NAME         Model name (default: deepseek-v3.1)
--provider NAME      Provider: openai|anthropic (default: openai)
--temperature FLOAT  Temperature (default: 0.0)
--head-lines N       Context lines to include (default: 500)
```

**Output Structure**:
```
baseline_outputs/
├── baseline_issue_001.diff
├── baseline_issue_001_tests.txt
├── baseline_issue_002.diff
├── baseline_issue_002_tests.txt
└── baseline_summary_report.json
```

### 3. rag_solution.py

**Purpose**: Resolves issues using RAG service with automatic retrieval.

**Key Features**:
- Automatic context retrieval from vector index
- TOP-4 relevance filtering (only uses 4 most relevant files)
- Enhanced system prompts for structure preservation
- Source node relevance score tracking
- Optimized token usage

**Usage Pattern**:
```bash
python rag_solution.py \
  --issues <FILE> \
  --index <NAME> \
  --output <DIR>
```

**Full Options**:
```
--issues FILE        Issues file (required)
--index NAME         RAG index name (required)
--output DIR         Output directory (default: rag_outputs)
--url URL            RAG service URL (default: http://localhost:5000)
--model NAME         Model name (default: deepseek-v3.1)
--timeout N          API timeout seconds (default: 300)
```

**Key Implementation Details**:

1. **Relevance Filtering** (rag_solution.py:385-445):
```python
MAX_FILES = 4  # Hard limit on files per issue
sorted_files = sorted(file_path_scores.items(), 
                     key=lambda x: x[1], reverse=True)
top_files = sorted_files[:MAX_FILES]
```

2. **System Prompt** (rag_solution.py:130-180):
```python
- NEVER delete copyright headers
- NEVER delete package declarations  
- NEVER delete import sections
- Provide COMPLETE file content
```

3. **API Configuration**:
```python
temperature: 0.0           # Deterministic
max_tokens: 40000          # Large context
context_token_ratio: 0.7   # 70% context, 30% response
```

**Output Structure**:
```
rag_outputs/
├── issue_001.diff
├── issue_001_tests.txt
├── issue_001_raw.txt (if parsing failed)
├── issue_002.diff
├── issue_002_tests.txt
└── rag_summary_report.json
```

### 4. code_benchmark.py

**Purpose**: Compares baseline and RAG results.

**Key Features**:
- Side-by-side comparison
- Success rate calculation
- Token efficiency analysis
- Statistical significance testing
- Detailed error categorization

**Usage Pattern**:
```bash
python code_benchmark.py \
  --baseline <REPORT> \
  --rag <REPORT> \
  --output <FILE>
```

## Step-by-Step Tutorial

### Scenario: Benchmarking KAITO Repository

#### Step 1: Prepare RAG Index

```bash
# Ensure RAG service is running
curl http://localhost:5000/health

# Load your repository index
curl -X POST http://localhost:5000/load/kaito_index
```

#### Step 2: Generate Test Issues

```bash
python generate_issues.py \
  --repo /path/to/kaito \
  --count 10 \
  --output test_issues.txt \
  --llm-url https://api.openai.com/v1 \
  --api-key $OPENAI_API_KEY \
  --model gpt-4
```

**Expected Output**:
```
📁 Scanning repository structure...
   Found 324 Go files
   Found 89 Python files
🎯 Identified 15 components
🤖 Generating 10 issues using LLM...
✅ Generated 10 issues
💾 Saved to test_issues.txt
```

#### Step 3: Run Baseline Benchmark

```bash
python resolve_issues_baseline.py \
  --repo /path/to/kaito \
  --issues test_issues.txt \
  --output baseline_results \
  --api-key $OPENAI_API_KEY \
  --model gpt-4 \
  --temperature 0.0
```

**Progress Indicators**:
```
📋 Loaded 10 issues from test_issues.txt
================================================================================
📝 Baseline Issue #1: Add error handling for nil workspace spec...
================================================================================
  📂 Reading repository files...
  🤖 Calling LLM API (gpt-4)...
  📊 Token usage: 12500 total (prompt: 8000, completion: 4500)
  ✓ Modified: api/v1beta1/workspace_validation.go
  💾 Diff saved to: baseline_results/baseline_issue_001.diff
  🧪 Running Go tests for packages: ./api/v1beta1
    Testing Go package ./api/v1beta1...
    ✓ Go tests passed for ./api/v1beta1
  💾 Test output saved to: baseline_results/baseline_issue_001_tests.txt
...
================================================================================
📊 BASELINE SUMMARY REPORT
================================================================================
Total Issues:        10
Tests Passed:        3 (30.0%)
Tests Failed:        5 (50.0%)
No Changes:          2 (20.0%)
```

#### Step 4: Run RAG Benchmark

```bash
python rag_solution.py \
  --issues test_issues.txt \
  --index kaito_index \
  --output rag_results \
  --url http://localhost:5000 \
  --model gpt-4
```

**Progress with Relevance Filtering**:
```
📋 Loaded 10 issues from test_issues.txt
================================================================================
📝 RAG Issue #1: Add error handling for nil workspace spec...
================================================================================
  🤖 Calling RAG API (gpt-4)...
  📊 RAG returned 16 source nodes
  📋 Relevance scores for all 16 files:
     ✓ TOP1: 0.5205 | api/v1beta1/workspace_validation.go
     ✓ TOP2: 0.5193 | api/v1beta1/workspace_validation_test.go
     ✓ TOP3: 0.5192 | api/v1alpha1/workspace_validation.go
     ✓ TOP4: 0.5177 | pkg/utils/workspace/workspace.go
     ✗ 0.4962 | pkg/controller/workspace_controller.go  (filtered)
     ✗ 0.4893 | pkg/utils/common.go  (filtered)
     ...
  ✅ Selected TOP 4 files, filtered out 12 lower-relevance files
  📁 Found 4 real file paths from RAG metadata
  ✓ Modified: api/v1beta1/workspace_validation.go
  🧪 Running tests...
  ✓ Tests passed
...
```

#### Step 5: Compare Results

```bash
python code_benchmark.py \
  --baseline baseline_results/baseline_summary_report.json \
  --rag rag_results/rag_summary_report.json \
  --output comparison_report.json
```

**Comparison Output**:
```json
{
  "comparison": {
    "baseline": {
      "success_rate": "30.0%",
      "total_tokens": 125000,
      "avg_tokens_per_issue": 12500
    },
    "rag": {
      "success_rate": "50.0%",
      "total_tokens": 98000,
      "avg_tokens_per_issue": 9800
    },
    "analysis": {
      "success_rate_diff": "+20.0%",
      "token_efficiency": "+21.6%",
      "winner": "RAG (by success rate and efficiency)"
    }
  }
}
```

## Advanced Usage

### Custom Issue Templates

Create `issue_templates.json`:
```json
[
  {
    "type": "error_handling",
    "description": "Add error handling for {component} in {module}",
    "requires": ["error handling", "validation"]
  },
  {
    "type": "performance",
    "description": "Optimize {operation} performance in {component}",
    "requires": ["profiling", "optimization"]
  }
]
```

Use with generator:
```bash
python generate_issues.py \
  --repo . \
  --templates issue_templates.json \
  --count 20
```

### Adjusting RAG Relevance Threshold

Edit `rag_solution.py`:
```python
# Line ~400
MAX_FILES = 4  # Change to 3 or 5 as needed
```

### Custom System Prompts

Edit `rag_solution.py` or `resolve_issues_baseline.py`:
```python
# Line ~130-180
system_message = {
    "role": "system",
    "content": """Your custom system prompt here..."""
}
```

## Best Practices

### 1. Issue Generation
- Start with small issue counts (5-10) for testing
- Use LLM-assisted generation for more realistic issues
- Review generated issues before running benchmarks
- Keep temperature at 0.7 for diverse but reasonable issues

### 2. Baseline Benchmarks
- **Always use temperature=0.0** for reproducibility
- Include sufficient context (head-lines=500 is good default)
- Monitor token usage to stay within API limits
- Run tests in isolated environment

### 3. RAG Benchmarks
- Ensure RAG index is fresh and complete
- Monitor relevance scores in logs
- Verify TOP-4 filtering is working
- Check that source_nodes contain metadata

### 4. Comparison
- Run multiple iterations for statistical significance
- Use same issues for both approaches
- Compare on multiple metrics (success rate, tokens, quality)
- Document any configuration differences

## Troubleshooting

### Issue: "RAG service connection refused"

**Cause**: RAG service not running or wrong URL

**Solution**:
```bash
# Check service
curl http://localhost:5000/health

# Start service if needed
cd presets/ragengine
python main.py --port 5000
```

### Issue: "No files modified" in RAG output

**Possible Causes**:
1. RAG index not loaded
2. Low relevance scores
3. RAG returned empty response

**Solutions**:
```bash
# 1. Load index
curl -X POST http://localhost:5000/load/your_index

# 2. Check logs for relevance scores
grep "📋 Relevance scores" rag_outputs/*.log

# 3. Check raw responses
cat rag_outputs/issue_001_raw.txt
```

### Issue: Tests failing with "package not found"

**Cause**: Modified files broke package structure

**Solution**:
- Check if copyright headers were preserved
- Verify package declarations intact
- Review system prompt enforcement
- Check RAG response completeness

### Issue: High token usage in baseline

**Solutions**:
- Reduce `--head-lines` parameter
- Be more selective with included files
- Use smaller context window models
- Filter out test files if not needed

### Issue: Low success rate in either approach

**Possible Causes**:
- Insufficient context provided
- Model not powerful enough
- Issues too complex or vague

**Solutions**:
- **For Baseline**: Increase `--head-lines` to provide more context
- **For RAG**: Verify system prompt in `rag_solution.py` (lines 130-180)
- Consider using a more capable model (GPT-4, Claude-3)
- Review and refine issue descriptions for clarity
- Check if test files are properly configured

## Performance Optimization

### Reducing Token Costs

1. **Baseline**: Reduce context size
```bash
--head-lines 300  # Instead of 500
```

2. **RAG**: Already optimized with TOP-4 filtering
```python
MAX_FILES = 3  # Further reduction if needed
```

### Improving Success Rates

1. **Better Issue Quality**: Use LLM-assisted generation
2. **More Context**: Increase head-lines or MAX_FILES
3. **Better Prompts**: Refine system messages
4. **Model Selection**: Try different models

### Parallel Execution

```bash
# Run baseline and RAG in parallel
python resolve_issues_baseline.py [...] &
python rag_solution.py [...] &
wait
```

## Conclusion

This benchmark suite provides comprehensive tools for comparing RAG and baseline LLM approaches. The key is to:

1. Generate realistic issues
2. Run both approaches with same configuration
3. Compare objectively on multiple metrics
4. Iterate and improve based on results

For questions or issues, refer to the main README.md or contact the maintainers.
