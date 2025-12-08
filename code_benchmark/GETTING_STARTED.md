# Getting Started with Code Benchmark

Quick start guide for running your first benchmark.

## Prerequisites

```bash
# Install dependencies
pip install openai anthropic requests

# Set API key
export OPENAI_API_KEY="your-api-key-here"

# Start RAG service (for RAG solution)
# cd presets/ragengine && python main.py
```

## 5-Minute Quickstart

### 1. Generate Test Issues (2 minutes)

```bash
python generate_issues.py \
  --repo /path/to/your/repo \
   --index kaito_code_benchmark \
  --count 5 \
  --output test_issues.txt
```

### 2. Run Baseline (10-15 minutes)

```bash
python resolve_issues_baseline.py \
  --repo /path/to/your/repo \
  --issues test_issues.txt \
  --output baseline_results \
  --api-key $OPENAI_API_KEY
```

### 3. Run RAG Solution (8-12 minutes)

```bash
# Ensure RAG service is running on http://localhost:5000
python rag_solution.py \
  --issues test_issues.txt \
  --index your_repo_index \
  --output rag_results
```

### 4. Compare Results (instant)

```bash
python code_benchmark.py \
  --baseline baseline_results/baseline_summary_report.json \
  --rag rag_results/rag_summary_report.json \
  --output comparison.json

# View results
cat comparison.json | python -m json.tool
```

## What You'll See

**Issue Generation**:
```
📁 Scanning repository structure...
   Found 324 Go files
🎯 Identified 15 components
✅ Generated 5 issues
```

**Baseline Execution**:
```
📝 Issue #1: Add error handling...
  🤖 Calling LLM...
  ✓ Modified: workspace_validation.go
  🧪 Tests passed
  
Success Rate: 40% (2/5)
```

**RAG Execution**:
```
📝 Issue #1: Add error handling...
  📊 RAG returned 16 source nodes
  ✓ TOP1: 0.5205 | workspace_validation.go
  ✓ TOP2: 0.5193 | workspace_validation_test.go
  ✓ TOP3: 0.5192 | workspace_types.go
  ✓ TOP4: 0.5177 | workspace_controller.go
  ✗ 12 files filtered out
  🧪 Tests passed
  
Success Rate: 60% (3/5)
```

## Next Steps

- 📚 Read [CODE_BENCHMARK_GUIDE.md](CODE_BENCHMARK_GUIDE.md) for detailed usage
- 🏗️ Read [CODE_BENCHMARK_ARCHITECTURE.md](CODE_BENCHMARK_ARCHITECTURE.md) for technical details
- 📊 Read [CODE_BENCHMARK_PRESENTATION.md](CODE_BENCHMARK_PRESENTATION.md) for overview slides

## Troubleshooting

**"RAG service connection refused"**:
```bash
curl http://localhost:5000/health
# Start RAG service if needed
```

**"No files modified"**:
- Check if RAG index is loaded
- Review relevance scores in logs
- Verify source_nodes in RAG response

**"Tests failing"**:
- Check if copyright headers preserved
- Verify package declarations intact
- Review system prompt configuration

## Support

For issues or questions:
- 📧 Contact: team@kaito-project.io
- 📂 Repository: github.com/kaito-project/kaito
- 📚 Docs: See documentation files in this directory
