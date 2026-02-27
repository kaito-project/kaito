# RAG Benchmark Testing Guide (Document Q&A)

## Overview

This benchmark tool helps you **quantitatively measure** how much RAG improves over pure LLM responses for **document-based question answering**. It generates test questions from your indexed documents, compares answers from both systems, and produces detailed performance reports.

> **Scope**: This tool is designed for benchmarking RAG on **pre-indexed documents**. It works with any content type (PDFs, reports, manuals, code, etc.) as long as the documents are already indexed in your RAG system.

> **Important**: Users must manually create and index documents in their RAG system before running this benchmark. The tool does NOT handle document indexing.

**Use Cases:**
- Evaluate if RAG is worth implementing for document Q&A use cases
- Measure RAG performance improvement on your specific indexed content (usually 100-300%+)
- Compare different RAG configurations for document retrieval
- Generate quantitative reports for stakeholders

---

## Quick Start

**Prerequisites:**
1. Index your documents in the RAG system first
2. Note the index name you created

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

---

## How It Works

### Architecture Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  STEP 1: Query Existing Index                                          │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                   [User provides --index-name]
                              │
                              ▼
                   ┌────────────────────────┐
                   │   Validate Index       │
                   │   Exists in RAG System │
                   └────────────┬───────────┘
                                │
┌─────────────────────────────────────────────────────────────────────────┐
│  STEP 2: Generate Test Questions                                       │
└─────────────────────────────────────────────────────────────────────────┘
                                │
                   Retrieve Indexed Nodes via API
                                │
                                ▼
                   ┌────────────────────────┐
                   │  Smart Node Sampling   │
                   │  (20 nodes selected)   │
                   └────────────┬───────────┘
                                │
                                ▼
                   ┌────────────────────────┐
                   │ LLM Generates Q&A      │
                   └────────────┬───────────┘
                                │
               ┌────────────────┴────────────────┐
               ▼                                 ▼
    ┌─────────────────────┐          ┌─────────────────────┐
    │ 10 Closed Questions │          │ 10 Open Questions   │
    │ (Factual)           │          │ (Analytical)        │
    │ Score: 0/5/10       │          │ Score: 0-10         │
    └─────────────────────┘          └─────────────────────┘
                                │
┌─────────────────────────────────────────────────────────────────────────┐
│  STEP 3: Run Benchmark Tests (20 questions × 2 systems)                │
└─────────────────────────────────────────────────────────────────────────┘
                                │
              ┌─────────────────┴─────────────────┐
              ▼                                   ▼
   ┌──────────────────────┐          ┌──────────────────────┐
   │   RAG System         │          │   Pure LLM           │
   │                      │          │                      │
   │ • Search index       │          │ • No context         │
   │ • Retrieve context   │          │ • Direct query       │
   │ • Generate answer    │          │ • Generate answer    │
   └──────────┬───────────┘          └──────────┬───────────┘
              │                                   │
              └─────────────┬─────────────────────┘
                            │
┌─────────────────────────────────────────────────────────────────────────┐
│  STEP 4: Evaluate with LLM Judge                                       │
└─────────────────────────────────────────────────────────────────────────┘
                            │
                            ▼
                   ┌────────────────────────┐
                   │  LLM Judge Scores      │
                   │  Each Answer: 0-10     │
                   │                        │
                   │  40 total evaluations  │
                   │  (20 RAG + 20 LLM)     │
                   └────────────┬───────────┘
                                │
┌─────────────────────────────────────────────────────────────────────────┐
│  STEP 5: Generate Report                                               │
└─────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
                   ┌────────────────────────┐
                   │  Performance Metrics   │
                   │                        │
                   │ • RAG avg score        │
                   │ • LLM avg score        │
                   │ • Improvement %        │
                   │ • Token efficiency     │
                   └────────────────────────┘
```

---

## Detailed Process

### Step 1: Use Existing Index
- **Index Required**: You must provide `--index-name` pointing to a pre-indexed collection in your RAG system
- **Pre-requisite**: Documents must be indexed beforehand using your RAG system's indexing API/tool
- **Flexibility**: Works with any content type (documents, code, web pages, etc.)

### Step 2: Test Question Generation
1. **Retrieve nodes** from RAG index via API
2. **Filter** content-rich nodes (min 200 chars, 30 words)
3. **Smart sampling**: 
   - If ≤20 nodes: distribute questions evenly
   - If >20 nodes: randomly select 20
4. **Generate Q&A pairs** using LLM:
   - 10 closed-ended (factual, specific answers)
   - 10 open-ended (comprehension, analysis)
   - Ground truth answers included

### Step 3: Comparative Testing
For each question, both systems generate answers:

**RAG System:**
- Performs vector similarity search
- Retrieves relevant context chunks
- Passes context + question to LLM
- Generates contextually-aware answer

**Pure LLM:**
- No document access
- Relies solely on pre-trained knowledge
- Often lacks specific details
- May hallucinate or be outdated

### Step 4: Evaluation
**LLM as Judge** evaluates each answer:

**Closed Questions (0/5/10 scoring):**
- 10 = Completely correct, all facts match
- 5 = Partially correct, missing details
- 0 = Wrong or irrelevant

**Open Questions (0-10 gradient):**
- Accuracy (3 pts)
- Completeness (3 pts)
- Understanding (2 pts)
- Relevance (2 pts)

### Step 5: Results Analysis
Reports include:
- Average scores (RAG vs LLM)
- Performance improvement percentage
- Category breakdown (closed vs open)
- Token usage efficiency
- Detailed Q&A with scores

---

## Output Files

All results saved to `benchmark_results/` directory:

```
benchmark_results/
├── questions_20251020_043345.json    # Generated test questions
├── results_20251020_043345.json      # Detailed answers & scores
├── report_20251020_043345.json       # JSON metrics
└── report_20251020_043345.txt        # Human-readable report
```

### Sample Report Output

```
RAG vs LLM Benchmark Report
==================================================

Total Questions: 20

Average Scores (0-10):
  RAG Overall: [Score]
  LLM Overall: [Score]
  Performance Improvement: [Percentage]

Closed Questions (0/5/10 scoring - factual accuracy):
  RAG: [Score]
  LLM: [Score]

Open Questions (0-10 scoring - comprehensive evaluation):
  RAG: [Score]
  LLM: [Score]

Token Usage:
  RAG: [Count] tokens
  LLM: [Count] tokens
  Efficiency: [Percentage] fewer/more tokens with RAG
```

---

## Configuration Options

```bash
python rag_benchmark_docs.py [OPTIONS]

Required:
  --index-name NAME       Name of existing RAG index (REQUIRED)

Service URLs:
  --rag-url URL           RAG service endpoint (default: http://localhost:5000)
  --llm-url URL           Pure LLM API endpoint (default: http://localhost:8081)
  --judge-url URL         Judge LLM API endpoint (default: http://localhost:8082)

Model Configuration:
  --llm-model NAME        Model name for LLM/RAG service (default: gpt-4)
  --judge-model NAME      Model name for Judge LLM (default: gpt-4)

Authentication:
  --llm-api-key KEY       API key for LLM service (or env: LLM_API_KEY)
  --judge-api-key KEY     API key for Judge LLM (or env: JUDGE_API_KEY)

Test Configuration:
  --closed-questions N    Number of closed questions (default: 10)
  --open-questions N      Number of open questions (default: 10)
  --output-dir DIR        Results directory (default: benchmark_results)
```

---

## Requirements

### Prerequisites
1. **Indexed Documents**: You must have already indexed your documents in the RAG system
2. **RAG Service**: Running RAG instance accessible via HTTP API
3. **LLM API**: Compatible with OpenAI Chat Completion format

### Python Dependencies
- **Python 3.8+** with dependencies:
  ```bash
  pip install requests tqdm
  ```

### Indexing Your Documents (Before Running Benchmark)

This tool does NOT handle document indexing. You must index documents beforehand using your RAG system's tools. Example workflow:

```bash
# Example 1: Using RAG API directly
curl -X POST http://localhost:5000/index \
  -H "Content-Type: application/json" \
  -d '{
    "index_name": "my_docs",
    "documents": [
      {"text": "Your document content...", "metadata": {...}}
    ]
  }'

# Example 2: Using a custom indexing script
python index_documents.py --input docs/ --index-name my_docs

# Then run benchmark with the index name
python rag_benchmark_docs.py --index-name my_docs
```

---

## Tips for Best Results

1. **Index quality matters**: Ensure your documents are properly chunked and indexed before benchmarking
2. **Use representative content**: Test with indexed content similar to production use cases
3. **Sufficient content**: Indexes with 20+ rich content nodes work best
4. **Quality over quantity**: 20 well-crafted questions > 100 random ones
5. **Consistent LLM**: Use same model for question generation and judging (via `--llm-model` and `--judge-model`)
6. **Review questions**: Check `questions_*.json` to ensure quality
7. **Model selection**: Configure model names explicitly if not using default "gpt-4"

---

## Expected Performance

Based on typical benchmarks:

| Metric | RAG | Pure LLM | Notes |
|--------|-----|----------|-------|
| **Closed Questions** | Higher | Lower | RAG has document access |
| **Open Questions** | Higher | Moderate | RAG provides context |
| **Token Usage** | Variable | Variable | Depends on retrieval |
| **Accuracy** | High | Limited | Document-specific knowledge |

**Key Finding**: RAG typically excels at factual accuracy (closed questions) where pure LLM lacks specific document knowledge.

---

## Troubleshooting

**Q: Getting "index not found" errors?**
- Verify the index name exists in your RAG system
- Check index name spelling matches exactly
- Ensure documents are properly indexed before running benchmark

**Q: Getting "Connection refused" errors?**
- Ensure RAG service is running on specified port
- Check LLM API endpoints are accessible
- Verify firewall/network settings

**Q: Getting "No documents found in index" errors?**
- Your index may be empty or have no content-rich nodes
- Re-index your documents with proper content
- Check that indexing process completed successfully

**Q: Questions seem low quality?**
- Index may have insufficient content-rich nodes (need 20+ nodes with 200+ chars)
- Try indexing more comprehensive documents
- Ensure documents have meaningful text content

**Q: Scores seem biased?**
- Use different LLM for judging vs generation
- Configure models explicitly with `--llm-model` and `--judge-model`
- Review scoring prompts for clarity

**Q: Process is slow?**
- Expected: ~10-15 minutes for 20 questions
- Reduce question count for faster testing: `--closed-questions 5 --open-questions 5`
- Each question requires 4 LLM calls (2 answers + 2 scores)

**Q: Token counts seem wrong?**
- Tool now extracts real token usage from API responses
- If API doesn't return token data, it falls back to estimation (with warning in logs)
- Check that your LLM API returns usage data in OpenAI format