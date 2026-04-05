# RAG Benchmark Tool (Document Testing)

Quick benchmark testing to measure RAG performance improvement over pure LLM **for document-based Q&A**.

> **Note**: This tool benchmarks RAG performance on **pre-indexed documents**. Works with any content type (documents, code, web pages, etc.) as long as it's indexed in your RAG system.

> **Important**: You must manually index your documents in the RAG system **BEFORE** running this benchmark. This tool does NOT handle indexing.

## 🚀 Quick Start

**Step 1: Index your documents first** (using your RAG system's tools)

**Step 2: Run benchmark with the index name**
```bash
python rag_benchmark_docs.py \
  --index-name my_docs_index \
  --rag-url http://localhost:5000 \
  --llm-url http://your-llm-api.com \
  --judge-url http://your-llm-api.com \
  --llm-model "deepseek-v3.1" \
  --judge-model "deepseek-v3.1" \
  --llm-api-key "your-api-key" \
  --judge-api-key "your-api-key"
```

## 📊 What You Get

```
RAG vs LLM Benchmark Report
==================================================

Total Questions: 20

Average Scores (0-10):
  RAG Overall: [Higher score]        ← Your RAG system
  LLM Overall: [Lower score]         ← Pure LLM baseline
  Performance Improvement: [Percentage]

Closed Questions (factual accuracy):
  RAG: [Higher - has access to documents]
  LLM: [Lower - no document context]

Open Questions (comprehensive):
  RAG: [Higher - contextually aware]
  LLM: [Lower - general knowledge only]

Token Usage:
  RAG: [Typically fewer tokens]
  LLM: [More tokens]
  Efficiency: [RAG usually more efficient]
```

## 📖 Documentation

- **[RAG_BENCHMARK_DOCS_GUIDE.md](./RAG_BENCHMARK_DOCS_GUIDE.md)** - Complete guide with architecture, configuration options, and troubleshooting
- **[benchmark_results/](../benchmark_results/)** - Results output directory

## 🎯 Key Features

- ✅ **Pre-indexed content**: Works with any indexed content in your RAG system
- ✅ **Smart sampling**: Automatically selects representative content from index
- ✅ **Dual question types**: 10 factual + 10 analytical questions
- ✅ **LLM-as-Judge**: Automated scoring with ground truth comparison
- ✅ **Detailed reports**: JSON + human-readable formats
- ✅ **Real token tracking**: Extracts actual token usage from API responses
- ✅ **Configurable models**: Specify any LLM model via `--llm-model` and `--judge-model`

## 📦 Requirements

```bash
pip install requests tqdm
```

- **Pre-indexed documents** in your RAG system (you must index them first!)
- Running RAG service with accessible index
- LLM API access (OpenAI-compatible format)

## ⏱️ Runtime

- ~10-15 minutes for 20 questions
- 4 LLM calls per question (2 answers + 2 scores)
- Adjustable with `--closed-questions` and `--open-questions` flags

## 🎓 Example Use Cases

1. **Prove ROI**: Show stakeholders quantitative RAG improvements
2. **A/B Testing**: Compare different RAG configurations or models
3. **Quality Control**: Ensure RAG maintains performance over time
4. **Documentation**: Generate reports for compliance/audits
5. **Model Comparison**: Test different LLM models with the same indexed content

## 📋 Workflow

```
1. Index your documents (manually, using your RAG system)
   ↓
2. Note the index name
   ↓
3. Run: python rag_benchmark_docs.py --index-name <name>
   ↓
4. Get detailed performance report
```

---

**Ready to benchmark?** Start with the [Quick Start](#-quick-start) above or read the [full guide](./RAG_BENCHMARK_DOCS_GUIDE.md) for advanced options.
