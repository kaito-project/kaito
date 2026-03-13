# Code Benchmark: System Architecture

## Overview

This document describes the architecture and design decisions of the Code Benchmark suite for comparing RAG and baseline LLM approaches in automated code modification.

## Complete Workflow

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          COMPLETE WORKFLOW                                │
└──────────────────────────────────────────────────────────────────────────┘

PREREQUISITE: Index Your Code Repository
┌────────────────────────────────────────────────────────────────────┐
│  python3 rag.py --repo . --url http://localhost:5000 \            │
│                 --index code_repo_benchmark                        │
│                                                                    │
│  Creates: Vector index of all code files (for RAG retrieval)      │
└────────────────────────────────────────────────────────────────────┘
                              ↓
STEP 1: Generate Test Issues from Indexed Code
┌────────────────────────────────────────────────────────────────────┐
│  python3 generate_issues.py --repo . --output test_issues.txt     │
│                                                                    │
│  Input:   Scanned code repository structure                       │
│  Process: Analyze → Identify components → Generate realistic      │
│           issues based on actual code structure                   │
│  Output:  test_issues.txt (5 issues)                              │
└────────────────────────────────────────────────────────────────────┘
                              ↓
STEP 2: Run Baseline Solution (Direct LLM with Manual Context)
┌────────────────────────────────────────────────────────────────────┐
│  python3 resolve_issues_baseline.py --issues test_issues.txt \    │
│                                      --output baseline_outputs/    │
│                                                                    │
│  Process:                                                          │
│    1. Read specified files (manual context)                       │
│    2. Call LLM with issue + context                               │
│    3. Parse JSON response (file modifications)                    │
│    4. Apply modifications to files                                │
│    5. Generate git diff                                           │
│    6. Run unit tests                                              │
│    7. Pass → Keep changes                                         │
│       Fail → Revert changes                                       │
│                                                                    │
│  Output:                                                           │
│    baseline_outputs/                                               │
│    ├── baseline_issue_001.diff       (git diff)                  │
│    ├── baseline_issue_001_tests.txt  (test results)              │
│    ├── baseline_issue_002.diff                                    │
│    ├── baseline_issue_002_tests.txt                               │
│    └── baseline_summary_report.json  (success rate, tokens)      │
└────────────────────────────────────────────────────────────────────┘
                              ↓
STEP 3: Run RAG Solution (Automatic Retrieval with TOP-4 Filtering)
┌────────────────────────────────────────────────────────────────────┐
│  python3 rag_solution.py --issues test_issues.txt \               │
│                          --output rag_outputs/                     │
│                                                                    │
│  Process:                                                          │
│    1. Call RAG service with issue                                 │
│    2. RAG retrieves 100+ docs internally                          │
│    3. RAG returns 4-16 source_nodes with relevance scores         │
│    4. **TOP-4 FILTERING**: Sort by score, take top 4 files only   │
│    5. Parse RAG response (file modifications)                     │
│    6. Apply modifications to files                                │
│    7. Generate git diff                                           │
│    8. Run unit tests                                              │
│    9. Pass → Keep changes                                         │
│       Fail → Revert changes                                       │
│                                                                    │
│  Innovation: TOP-4 Filtering                                       │
│    • RAG returns 16 files: [0.5205, 0.4962, 0.4751, ...]         │
│    • Sort descending by relevance score                           │
│    • Take only TOP 4 → 21.6% token savings                        │
│    • Improves context quality                                     │
│    • Log: "✓ TOP1: 0.5205 | file.go"                             │
│           "✗ 0.4751 | other.go (filtered)"                        │
│                                                                    │
│  Output:                                                           │
│    rag_outputs/                                                    │
│    ├── rag_issue_001.diff            (git diff)                  │
│    ├── rag_issue_001_tests.txt       (test results)              │
│    ├── rag_issue_002.diff                                         │
│    ├── rag_issue_002_tests.txt                                    │
│    └── rag_summary_report.json       (success rate, tokens)      │
└────────────────────────────────────────────────────────────────────┘
                              ↓
STEP 4: Compare Results & Generate Report
┌────────────────────────────────────────────────────────────────────┐
│  python3 code_benchmark.py --baseline baseline_outputs/ \         │
│                             --rag rag_outputs/ \                   │
│                             --output comparison_report.json        │
│                                                                    │
│  Process:                                                          │
│    1. Load both summary reports (JSON)                            │
│    2. Calculate metrics:                                          │
│       • Success Rate: Pass/Total                                  │
│       • Token Efficiency: Avg tokens per issue                    │
│       • Files Modified: Number of changed files                   │
│       • Error Categories: Compilation errors, test failures       │
│    3. Compare baseline vs RAG                                     │
│    4. Determine winner                                            │
│    5. Generate recommendations                                    │
│                                                                    │
│  Output:                                                           │
│    comparison_report.json                                          │
│    {                                                               │
│      "baseline": {                                                 │
│        "success_rate": 0.20,                                       │
│        "avg_tokens": 12543,                                        │
│        "files_modified": 3                                         │
│      },                                                            │
│      "rag": {                                                      │
│        "success_rate": 0.60,                                       │
│        "avg_tokens": 9842,                                         │
│        "files_modified": 4                                         │
│      },                                                            │
│      "winner": "rag",                                              │
│      "token_savings": "21.6%",                                     │
│      "recommendations": [                                          │
│        "RAG provides better context coverage",                    │
│        "TOP-4 filtering balances quality and efficiency",        │
│        "Automatic retrieval outperforms manual selection"         │
│      ]                                                             │
│    }                                                               │
└────────────────────────────────────────────────────────────────────┘
```

## System Design

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Code Benchmark Suite                         │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
┌───────────────┐    ┌──────────────┐    ┌──────────────┐
│    Issue      │    │   Baseline   │    │     RAG      │
│  Generator    │    │   Solution   │    │   Solution   │
└───────────────┘    └──────────────┘    └──────────────┘
        │                     │                     │
        │                     ▼                     ▼
        │            ┌─────────────┐      ┌─────────────┐
        │            │     LLM     │      │ RAG Service │
        │            │   API       │      │   + LLM     │
        │            └─────────────┘      └─────────────┘
        │                     │                     │
        └─────────────────────┴─────────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │    Benchmark     │
                    │   Comparison     │
                    └──────────────────┘
```

## Component Details

### 1. Issue Generator (`generate_issues.py`)

**Purpose**: Generate realistic test issues based on repository analysis

**Architecture**:

```python
┌────────────────────────────────────────────┐
│         CodebaseAnalyzer                   │
│  ┌──────────────────────────────────────┐ │
│  │  scan_repository()                   │ │
│  │  - Walk directory tree               │ │
│  │  - Identify Go/Python files          │ │
│  │  - Build structure map               │ │
│  └──────────────────────────────────────┘ │
│                                            │
│  ┌──────────────────────────────────────┐ │
│  │  analyze_components()                │ │
│  │  - Extract packages/modules          │ │
│  │  - Identify controllers/services     │ │
│  │  - Map dependencies                  │ │
│  └──────────────────────────────────────┘ │
└────────────────────────────────────────────┘
                    │
                    ▼
┌────────────────────────────────────────────┐
│         IssueGenerator                     │
│  ┌──────────────────────────────────────┐ │
│  │  generate_issues()                   │ │
│  │  - Use templates                     │ │
│  │  - Fill with component names         │ │
│  │  - Optional: LLM enhancement         │ │
│  └──────────────────────────────────────┘ │
└────────────────────────────────────────────┘
```

**Key Design Decisions**:

1. **Template-Based Generation**: Uses predefined templates to ensure issue quality
2. **Structure-Aware**: Analyzes actual codebase to generate relevant issues
3. **LLM Enhancement**: Optional LLM call for smarter, more realistic issues
4. **Language-Agnostic**: Supports multiple languages (Go, Python, etc.)

**Data Flow**:
```
Repository → Scanner → Components → Templates → Issues
                 ↓
            (Optional)
                LLM → Enhanced Issues
```

### 2. Baseline Solution (`resolve_issues_baseline.py`)

**Purpose**: Resolve issues using direct LLM calls with manual context

**Architecture**:

```python
┌─────────────────────────────────────────────────┐
│         BaselineCodeModifier                    │
│  ┌───────────────────────────────────────────┐ │
│  │  read_relevant_files()                    │ │
│  │  - Identify files from issue context     │ │
│  │  - Read file contents                    │ │
│  │  - Limit to head_lines                   │ │
│  └───────────────────────────────────────────┘ │
│                                                 │
│  ┌───────────────────────────────────────────┐ │
│  │  call_llm()                               │ │
│  │  - System prompt (structure rules)       │ │
│  │  - User prompt (issue + context)         │ │
│  │  - Temperature = 0.0                     │ │
│  │  - Parse JSON response                   │ │
│  └───────────────────────────────────────────┘ │
│                                                 │
│  ┌───────────────────────────────────────────┐ │
│  │  apply_modifications()                    │ │
│  │  - Write modified files                  │ │
│  │  - Generate git diffs                    │ │
│  │  - Run tests                             │ │
│  │  - Revert if tests fail                  │ │
│  └───────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

**Key Design Decisions**:

1. **Manual Context**: Developer provides file list, ensuring relevant context
2. **Temperature 0.0**: Deterministic output for reproducibility
3. **Head Lines Limiting**: Control token usage by limiting file lengths
4. **Test Validation**: Automatic compilation and test execution
5. **Auto-Revert**: Rolls back changes if tests fail

**Data Flow**:
```
Issue → File Reader → Context Builder → LLM API
                                         ↓
                                    JSON Response
                                         ↓
        Test Results ← Test Runner ← File Writer
                                         ↓
                                    Git Diff
```

### 3. RAG Solution (`rag_solution.py`)

**Purpose**: Resolve issues using RAG service with automatic retrieval

**Architecture**:

```python
┌──────────────────────────────────────────────────┐
│         RAGCodeModifier                          │
│  ┌────────────────────────────────────────────┐ │
│  │  call_rag()                                │ │
│  │  - Send issue to RAG API                  │ │
│  │  - RAG retrieves 100+ documents internally│ │
│  │  - Returns top-k source_nodes with scores │ │
│  └────────────────────────────────────────────┘ │
│                      │                           │
│                      ▼                           │
│  ┌────────────────────────────────────────────┐ │
│  │  _fix_file_paths_from_metadata()          │ │
│  │  - Extract source_nodes from response     │ │
│  │  - Read relevance scores                  │ │
│  │  - Sort by score (descending)             │ │
│  │  - Select TOP 4 files ONLY                │ │
│  │  - Filter out low-relevance files         │ │
│  └────────────────────────────────────────────┘ │
│                      │                           │
│                      ▼                           │
│  ┌────────────────────────────────────────────┐ │
│  │  _parse_rag_response()                     │ │
│  │  - Parse JSON from RAG                    │ │
│  │  - Handle deepseek-specific format       │ │
│  │  - Extract file modifications            │ │
│  └────────────────────────────────────────────┘ │
│                      │                           │
│                      ▼                           │
│  ┌────────────────────────────────────────────┐ │
│  │  apply_modifications()                     │ │
│  │  - Write files                            │ │
│  │  - Generate diffs                         │ │
│  │  - Run tests                              │ │
│  │  - Revert if failed                       │ │
│  └────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
```

**Key Design Decisions**:

1. **Automatic Retrieval**: No manual file selection needed
2. **TOP-4 Filtering**: Hard limit on context to prevent overload
3. **Relevance-Based**: Uses cosine similarity scores from RAG
4. **Enhanced System Prompt**: Strong warnings about structure preservation
5. **Source Node Validation**: Ensures metadata is available

**Critical Implementation Details**:

```python
# Relevance Filtering (Lines 385-445)
def _fix_file_paths_from_metadata(self, parsed_response, rag_result):
    MAX_FILES = 4  # Hard limit
    
    # Extract scores
    file_path_scores = {}
    for node in rag_result.get('source_nodes', []):
        score = node.get('score', 0.0)
        file_path = node['metadata']['file_path']
        file_path_scores[file_path] = score
    
    # Sort and filter
    sorted_files = sorted(file_path_scores.items(), 
                         key=lambda x: x[1], 
                         reverse=True)
    top_files = sorted_files[:MAX_FILES]
    
    # Log filtering
    print(f"  📋 Relevance scores for all {len(sorted_files)} files:")
    for i, (path, score) in enumerate(sorted_files, 1):
        if i <= MAX_FILES:
            print(f"     ✓ TOP{i}: {score:.4f} | {path}")
        else:
            print(f"     ✗ {score:.4f} | {path}")
    
    return {path for path, score in top_files}
```

**RAG Service Integration**:

```
┌──────────────────────────────────────────┐
│         RAG Service (Port 5000)          │
│  ┌────────────────────────────────────┐ │
│  │  /v1/chat/completions              │ │
│  │  - Receives: messages, model, etc. │ │
│  │  - Returns: response + source_nodes│ │
│  └────────────────────────────────────┘ │
│                │                         │
│                ▼                         │
│  ┌────────────────────────────────────┐ │
│  │  Vector Store Query                │ │
│  │  - Calculate: top_k = max(100, ...) │ │
│  │  - Retrieve 100+ documents         │ │
│  │  - Rank by similarity              │ │
│  └────────────────────────────────────┘ │
│                │                         │
│                ▼                         │
│  ┌────────────────────────────────────┐ │
│  │  LLM Context Building              │ │
│  │  - Include top documents           │ │
│  │  - Build prompt                    │ │
│  │  - Call LLM                        │ │
│  └────────────────────────────────────┘ │
│                │                         │
│                ▼                         │
│  ┌────────────────────────────────────┐ │
│  │  Response Assembly                 │ │
│  │  - LLM response                    │ │
│  │  - Source nodes with metadata      │ │
│  │  - Relevance scores                │ │
│  └────────────────────────────────────┘ │
└──────────────────────────────────────────┘
```

**Data Flow**:
```
Issue → RAG API → Internal Retrieval (100+ docs)
                        ↓
                 Rank by Similarity
                        ↓
                 Build LLM Context
                        ↓
                 LLM Generation
                        ↓
              Response + Source Nodes
                        ↓
       Python Client (TOP-4 Filter)
                        ↓
            Apply Modifications
```

### 4. Benchmark Comparison (`code_benchmark.py`)

**Purpose**: Compare results from baseline and RAG solutions

**Architecture**:

```python
┌────────────────────────────────────────┐
│      BenchmarkComparator               │
│  ┌──────────────────────────────────┐ │
│  │  load_reports()                  │ │
│  │  - Parse baseline JSON           │ │
│  │  - Parse RAG JSON                │ │
│  └──────────────────────────────────┘ │
│                │                       │
│                ▼                       │
│  ┌──────────────────────────────────┐ │
│  │  compare_success_rates()         │ │
│  │  - Pass vs Fail counts           │ │
│  │  - Percentage calculation        │ │
│  │  - Statistical significance      │ │
│  └──────────────────────────────────┘ │
│                │                       │
│                ▼                       │
│  ┌──────────────────────────────────┐ │
│  │  compare_token_usage()           │ │
│  │  - Total tokens                  │ │
│  │  - Average per issue             │ │
│  │  - Efficiency ratio              │ │
│  └──────────────────────────────────┘ │
│                │                       │
│                ▼                       │
│  ┌──────────────────────────────────┐ │
│  │  analyze_errors()                │ │
│  │  - Categorize failure types      │ │
│  │  - Common patterns               │ │
│  │  - Recommendations               │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────┘
```

## Design Patterns

### 1. Template Method Pattern

Used in both baseline and RAG solutions:

```python
class CodeModifier:
    def resolve_issue(self, issue):
        # Template method
        context = self.get_context(issue)      # Abstract
        response = self.call_ai(issue, context)  # Abstract
        self.apply_modifications(response)      # Concrete
        self.run_tests()                        # Concrete
        self.generate_report()                  # Concrete
```

### 2. Strategy Pattern

Different AI strategies (baseline vs RAG):

```python
class BaselineStrategy:
    def get_context(self, issue):
        return self.read_files_manually()

class RAGStrategy:
    def get_context(self, issue):
        return self.retrieve_from_index()
```

### 3. Observer Pattern

Progress tracking:

```python
class ProgressTracker:
    def notify(self, event, data):
        print(f"  {event}: {data}")

modifier.add_observer(ProgressTracker())
```

## Configuration Management

### Environment Variables

```bash
# LLM Configuration
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-...
LLM_MODEL=gpt-4

# RAG Configuration
RAG_SERVICE_URL=http://localhost:5000
RAG_INDEX_NAME=my_repo_index

# Benchmark Configuration
TEMPERATURE=0.0
MAX_TOKENS=40000
```

### Runtime Configuration

```python
# Baseline
baseline_config = {
    'head_lines': 500,
    'temperature': 0.0,
    'model': 'gpt-4'
}

# RAG
rag_config = {
    'max_files': 4,
    'temperature': 0.0,
    'context_token_ratio': 0.7
}
```

## Performance Considerations

### Token Optimization

**Baseline**:
- Limit file length with `head_lines`
- Selective file inclusion
- Efficient prompt structure

**RAG**:
- TOP-4 filtering (hard limit)
- Relevance score threshold
- Context/response ratio tuning

### Scalability

**Parallel Processing**:
```python
# Process multiple issues in parallel
from concurrent.futures import ThreadPoolExecutor

with ThreadPoolExecutor(max_workers=3) as executor:
    futures = [executor.submit(resolve_issue, issue) 
               for issue in issues]
```

**Rate Limiting**:
```python
import time

def with_rate_limit(func):
    def wrapper(*args, **kwargs):
        time.sleep(1)  # 1 second delay
        return func(*args, **kwargs)
    return wrapper
```

## Error Handling

### Retry Strategy

```python
def call_with_retry(func, max_retries=3):
    for retry in range(max_retries):
        try:
            return func()
        except Exception as e:
            if retry < max_retries - 1:
                wait = 2 ** retry  # Exponential backoff
                print(f"  ⚠️ Retrying in {wait}s...")
                time.sleep(wait)
            else:
                raise
```

### Graceful Degradation

```python
def resolve_issue_safe(issue):
    try:
        return resolve_issue(issue)
    except APIError:
        print("  ✗ API failed, saving raw response")
        save_raw_response()
        return None
    except TestError:
        print("  ✗ Tests failed, reverting changes")
        revert_changes()
        return None
```

## Testing Strategy

### Unit Tests

```python
def test_relevance_filtering():
    nodes = [
        {'score': 0.9, 'metadata': {'file_path': 'a.go'}},
        {'score': 0.8, 'metadata': {'file_path': 'b.go'}},
        {'score': 0.7, 'metadata': {'file_path': 'c.go'}},
        {'score': 0.6, 'metadata': {'file_path': 'd.go'}},
        {'score': 0.5, 'metadata': {'file_path': 'e.go'}},
    ]
    
    filtered = filter_top_k(nodes, k=4)
    assert len(filtered) == 4
    assert filtered[0]['file_path'] == 'a.go'
```

### Integration Tests

```python
def test_end_to_end():
    # Generate issues
    issues = generate_issues(repo='test_repo', count=2)
    
    # Run baseline
    baseline_results = resolve_baseline(issues)
    
    # Run RAG
    rag_results = resolve_rag(issues)
    
    # Compare
    comparison = compare(baseline_results, rag_results)
    
    assert comparison.success_rate > 0
```

## Future Enhancements

### Planned Features

1. **Multi-Model Support**: Test multiple LLMs in parallel
2. **Custom Metrics**: User-defined success criteria
3. **Confidence Scores**: RAG should return confidence for each change
4. **Interactive Mode**: Human-in-the-loop validation
5. **Continuous Benchmarking**: Automated daily runs

### Architectural Improvements

1. **Plugin System**: Easy addition of new AI strategies
2. **Database Backend**: Store results in SQLite/Postgres
3. **Web Dashboard**: Real-time progress monitoring
4. **API Layer**: RESTful API for remote execution

## Conclusion

The Code Benchmark architecture is designed for:

- **Modularity**: Easy to extend with new strategies
- **Reproducibility**: Deterministic results (temperature=0.0)
- **Observability**: Detailed logging and reporting
- **Scalability**: Parallel execution support
- **Robustness**: Comprehensive error handling

The key innovation is the **TOP-4 relevance filtering** in RAG solution, which balances context quality with token efficiency.
