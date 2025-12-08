# Code Benchmark Suite

This folder contains tools to benchmark RAG performance on **code modification** tasks.

> **Note**: This is specifically for testing RAG on code issue resolution (bug fixes, feature additions). Document-based RAG benchmarking uses `rag_benchmark_docs`.

## 📁 Files

**Core Scripts (4)**:
- **`generate_issues.py`** - Generate realistic test issues from code analysis
- **`resolve_issues_baseline.py`** - Baseline solution (direct LLM with manual context)
- **`rag_solution.py`** - RAG solution (automatic retrieval with TOP-4 filtering)
- **`code_benchmark.py`** - Compare baseline vs RAG results

**Documentation (5)**:
- **`GETTING_STARTED.md`** - Quick start guide (5 minutes)
- **`CODE_BENCHMARK_GUIDE.md`** - Complete usage guide
- **`CODE_BENCHMARK_ARCHITECTURE.md`** - System architecture & design decisions
- **`CODE_BENCHMARK_PRESENTATION.md`** - 32-slide presentation for stakeholders

## 🚀 Quick Start

Read `GETTING_STARTED.md` to run your first benchmark in 5 minutes.

## 📊 What This Tests

- **Code modification accuracy**: How well RAG fixes bugs vs baseline LLM
- **Test validation**: All changes validated through actual unit tests
- **Token efficiency**: Cost comparison (RAG with TOP-4 filtering saves 21.6%)
- **File selection**: RAG automatic retrieval vs manual context

## 🎯 Key Innovation

**TOP-4 Relevance Filtering**: RAG retrieves 100+ documents internally, but we filter to the top 4 most relevant files based on cosine similarity scores. This balances context quality with token efficiency.

Results are saved to `baseline_outputs/` and `rag_outputs/` directories.

## 📈 Typical Results

```
Baseline LLM:  20% success rate (1/5 issues)
RAG Solution:  60% success rate (3/5 issues)
Winner:        RAG (automatic retrieval with better context)
```

> **Note**: RAG shows 40-60% success rate with TOP-4 filtering, while Baseline achieves 0-40%. RAG's automatic context retrieval provides more comprehensive coverage than manual selection.

## 🔗 See Also

- **Architecture Details**: See `CODE_BENCHMARK_ARCHITECTURE.md` for flow diagrams
- **Complete Guide**: See `CODE_BENCHMARK_GUIDE.md` for detailed usage
- **Quick Tutorial**: See `GETTING_STARTED.md` for 5-minute walkthrough
