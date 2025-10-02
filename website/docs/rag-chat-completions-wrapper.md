# RAGEngine Chat Completion Algorithm

The `chat_completion` method implements a sophisticated filtering and message processing algorithm to determine when to use RAG (Retrieval-Augmented Generation) versus passing requests directly to the LLM. Here's how it works:

## Algorithm Overview

The method follows this decision tree:

```
1. Validate index existence and parameters
2. Convert to OpenAI format
3. Check bypass conditions (no index, tools, unsupported content)
4. Extract and validate messages
5. Process messages to separate user prompts from chat history
6. Validate token limits
7. Execute RAG-enabled chat completion
```

## Bypass Conditions (Pass-through to LLM)

The algorithm will **bypass RAG** and send requests directly to the LLM in these cases:

### 1. No Index Specified
```json
{
  "model": "gpt-4",
  "messages": [{"role": "user", "content": "Hello, how are you?"}]
}
```
**Result**: ✅ Pass-through (no `index_name` provided)

### 2. Tools or Functions Present
```json
{
  "model": "gpt-4",
  "index_name": "my_index",
  "messages": [{"role": "user", "content": "What's the weather?"}],
  "tools": [{"type": "function", "function": {"name": "get_weather"}}]
}
```
**Result**: ✅ Pass-through (contains `tools`)

### 3. Unsupported Message Roles
```json
{
  "model": "gpt-4",
  "index_name": "my_index", 
  "messages": [
    {"role": "function", "content": "Weather data: 75°F"},
    {"role": "user", "content": "Thanks!"}
  ]
}
```
**Result**: ✅ Pass-through (`function` role not supported for RAG)

### 4. Non-Text Content in User Messages
```json
{
  "model": "gpt-4",
  "index_name": "my_index",
  "messages": [
    {
      "role": "user", 
      "content": [
        {"type": "text", "text": "What's in this image?"},
        {"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
      ]
    }
  ]
}
```
**Result**: ✅ Pass-through (contains image content)

## RAG Processing Cases

When none of the bypass conditions are met, the algorithm processes messages for RAG:

### Message Processing Algorithm

The method processes messages in **reverse chronological order** to:
1. Find all consecutive user messages since the last assistant message
2. Combine these user messages into a single search query
3. Keep all other messages as chat history for context

```python
# Pseudo-code logic:
for message in reversed(messages):
    if message.role == "user" and not assistant_message_found:
        user_messages_for_prompt.insert(0, message.content)
    else:
        if message.role == "assistant":
            assistant_message_found = True
        chat_history.insert(0, message)
```

### Example 1: Simple User Query
```json
{
  "model": "gpt-4",
  "index_name": "docs_index",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "What is KAITO?"}
  ]
}
```

**Processing**:
- **user_prompt**: `"What is KAITO?"` (used for vector search)
- **chat_history**: `[system_message]` (context for LLM)
- **Result**: ✅ RAG processing with vector search on "What is KAITO?"

### Example 2: Multi-turn Conversation
```json
{
  "model": "gpt-4", 
  "index_name": "docs_index",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "What is KAITO?"},
    {"role": "assistant", "content": "KAITO is a Kubernetes operator for AI workloads."},
    {"role": "user", "content": "How do I install it?"}
  ]
}
```

**Processing**:
- **user_prompt**: `"How do I install it?"` (latest user message for vector search)
- **chat_history**: `[system_message, user_message("What is KAITO?"), assistant_message("KAITO is...")]`
- **Result**: ✅ RAG processing with vector search on installation question, full conversation context preserved

### Example 3: Multiple Consecutive User Messages
```json
{
  "model": "gpt-4",
  "index_name": "docs_index", 
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "What is KAITO?"},
    {"role": "assistant", "content": "KAITO is a Kubernetes operator."},
    {"role": "user", "content": "Tell me more about it."},
    {"role": "user", "content": "Specifically about GPU support."}
  ]
}
```

**Processing**:
- **user_prompt**: `"Tell me more about it.\n\nSpecifically about GPU support."` (combined consecutive user messages)
- **chat_history**: `[system_message, user_message("What is KAITO?"), assistant_message("KAITO is...")]`
- **Result**: ✅ RAG processing with vector search on combined user query

### Example 4: No User Messages After Assistant
```json
{
  "model": "gpt-4",
  "index_name": "docs_index",
  "messages": [
    {"role": "user", "content": "What is KAITO?"}, 
    {"role": "assistant", "content": "KAITO is a Kubernetes operator."}
  ]
}
```

**Processing**:
- **user_prompt**: `""` (empty - no user messages after assistant)
- **Result**: ❌ Error 400 - "There must be a user prompt since the latest assistant message."

## Token Validation

After message processing, the algorithm validates token limits and manages the `max_tokens` parameter:

### Understanding `max_tokens`

The `max_tokens` parameter is a standard OpenAI API parameter that controls the maximum number of tokens the model can generate in its response. It serves several important purposes:

**Why callers use `max_tokens`:**
- **Cost Control**: Limit token usage to manage API costs
- **Response Length Control**: Ensure responses fit within application constraints (UI limits, memory, etc.)
- **Performance Optimization**: Shorter responses generate faster
- **Predictable Behavior**: Guarantee responses won't exceed expected length

**Default Behavior:**
- If `max_tokens` is **not specified**: The model will generate until it naturally completes the response or hits the context window limit
- If `max_tokens` is **specified**: The model will stop generating when it reaches this limit, even if the response is incomplete

### Token Validation Process

The algorithm performs two critical validations:

#### 1. Context Window Check
```python
if prompt_len > self.llm.metadata.context_window:
    # Error: Prompt too long - reject request
    raise HTTPException(status_code=400, detail="Prompt length exceeds context window.")
```

**What this validates:**
- The entire conversation history + system prompts must fit within the model's context window
- If this check fails, the request is rejected entirely (no RAG processing possible)

#### 2. Max Tokens Adjustment
```python
if max_tokens and max_tokens > context_window - prompt_len:
    # Automatically adjust max_tokens to fit available space
    logger.warning(f"max_tokens ({max_tokens}) is greater than available context after prompt consideration. Setting to {context_window - prompt_len}.")
    max_tokens = context_window - prompt_len
```

**What this handles:**
- Ensures `max_tokens` + prompt length doesn't exceed the context window
- **Automatically adjusts** `max_tokens` downward if needed (with warning logged)
- Prevents context window overflow during RAG execution

### Examples of `max_tokens` Behavior

#### Example 1: No `max_tokens` Specified
```json
{
  "model": "gpt-4",
  "index_name": "docs_index",
  "messages": [{"role": "user", "content": "Explain KAITO in detail"}]
}
```
**Result**: Model generates until natural completion, using a calculated amount of context space for retrieved documents.

#### Example 2: Conservative `max_tokens`
```json
{
  "model": "gpt-4", 
  "index_name": "docs_index",
  "messages": [{"role": "user", "content": "Explain KAITO in detail"}],
  "max_tokens": 100
}
```
**Result**: Response limited to 100 tokens, more space available for retrieved context, potentially better RAG quality.

#### Example 3: Excessive `max_tokens`
```json
{
  "model": "gpt-4",
  "index_name": "docs_index", 
  "messages": [{"role": "user", "content": "Explain KAITO"}],
  "max_tokens": 8000
}
```
**Processing** (assuming 8192 context window, 500 token prompt):
- **Original**: `max_tokens = 8000`
- **Available space**: `8192 - 500 = 7692 tokens`
- **Adjusted**: `max_tokens = 7692` (with warning logged)
- **Result**: Automatic adjustment prevents context overflow


## RAG Execution

The final stage of the algorithm involves sophisticated document retrieval and context management through a multi-layered approach:

### 1. Dynamic Top-K Calculation

Before retrieving documents, the system calculates how many documents to fetch from the vector store:

```python
# Calculate initial retrieval size based on available context window
top_k = max(100, int((context_window - prompt_len) / RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION))
```

**Key Points:**
- **Minimum**: Always retrieves at least 100 documents for good recall
- **Token-Based Scaling**: Larger available context windows allow more document retrieval
- **Approximation Factor**: Uses 500 tokens per document node as estimation (configurable via `RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION`)
- **Latency Optimization**: Calculation considers that FAISS (in-memory) can handle larger retrievals efficiently

### 2. ContextSelectionProcessor: Intelligent Document Filtering

The `ContextSelectionProcessor` is the core component that intelligently selects which retrieved documents to include in the final prompt. It operates through a sophisticated token management and relevance filtering system:

#### Token Allocation Strategy

```python
# 1. Calculate base available tokens
available_tokens = context_window - query_tokens - ADDITION_PROMPT_TOKENS

# 2. Apply max_tokens constraint (if specified)
available_tokens = min(max_tokens or context_window, available_tokens)

# 3. Apply context fill ratio
final_context_tokens = int(available_tokens * context_token_ratio)
```

**Token Management Layers:**

1. **Query Token Calculation**: Uses actual LLM token counting for the user query
2. **Buffer Management**: Reserves 150 tokens (`ADDITION_PROMPT_TOKENS`) for LlamaIndex prompt formatting
3. **Max Tokens Constraint**: Respects the user's `max_tokens` parameter if specified
4. **Context Fill Ratio**: Applies the `context_token_ratio` (default: 0.5, range: 0.2-0.8)

#### Context Fill Ratio Explained

The `context_token_ratio` parameter controls what percentage of available token space is used for retrieved documents:

```json
{
  "context_token_ratio": 0.5,  // Use 50% of available space for context
  "context_token_ratio": 0.8,  // Use 80% of available space for context (max)
  "context_token_ratio": 0.2   // Use 20% of available space for context (min)
}
```

**Why this matters:**
- **Higher ratios (0.6-0.8)**: More context, potentially better answers, but less room for response generation
- **Lower ratios (0.2-0.4)**: Less context, more room for detailed responses
- **Balanced approach (0.5)**: Default provides good balance between context richness and response space

#### Document Selection Algorithm

The processor applies multiple filters in sequence:

```python
# 1. Sort by relevance (FAISS returns distance scores - lower is better)
ranked_nodes = sorted(nodes, key=lambda x: x.score or 0.0)

# 2. Apply similarity threshold filter (if configured)
if similarity_threshold and node.score > similarity_threshold:
    continue  # Skip nodes that don't meet relevance threshold

# 3. Apply token-based selection
node_tokens = llm.count_tokens(node.text)
if node_tokens > available_context_tokens:
    continue  # Skip nodes that would exceed token budget

# 4. Deduct tokens and include node
available_context_tokens -= node_tokens
result.append(node)
```

**Selection Criteria:**
1. **Relevance Ranking**: Documents are sorted by similarity score (most relevant first)
2. **Similarity Threshold**: Optional filter (default: 0.85) removes low-relevance documents
3. **Token Fitting**: Only includes documents that fit within the calculated token budget
4. **Greedy Selection**: Selects documents in relevance order until token budget is exhausted

### 3. Chat Engine Execution

Finally, the system executes the RAG-enabled chat completion:

```python
chat_engine = index.as_chat_engine(
    llm=self.llm,
    similarity_top_k=top_k,           # Initial retrieval size
    chat_mode=ChatMode.CONTEXT,      # Context-only mode (no query condensation)
    node_postprocessors=[
        ContextSelectionProcessor(
            rag_context_token_fill_ratio=context_token_ratio,
            llm=self.llm,
            max_tokens=max_tokens,
            similarity_threshold=0.85,
        )
    ],
)

# Execute with separated concerns
chat_result = await chat_engine.achat(user_prompt, chat_history=chat_history)
```

**Execution Flow:**
1. **Vector Search**: `user_prompt` is used for semantic search against the index
2. **Initial Retrieval**: Fetches `top_k` most similar documents from vector store
3. **Context Selection**: `ContextSelectionProcessor` filters down to final document set
4. **Prompt Construction**: Selected documents are formatted into the final prompt
5. **LLM Generation**: Complete prompt (context + history + user query) sent to LLM

**In the event that no relevant context is found, the LlamaIndex library will return an empty response. We look out for this and send the request directly to the LLM without context as described in the bypass handling above.**

### 4. Token Budget Example

Here's how token allocation works in practice:

```
Scenario: 8192 context window, 500 token conversation, max_tokens=1000, context_token_ratio=0.6

1. Base calculation:
   Available = 8192 - 500 (conversation) - 150 (formatting) = 7542 tokens

2. Apply max_tokens constraint:
   Available = min(1000, 7542) = 1000 tokens

3. Apply context ratio:
   Context budget = 1000 * 0.6 = 600 tokens for retrieved documents
   Response budget = 1000 - 600 = 400 tokens for LLM response

4. Document selection:
   - Retrieve top_k documents (100 as a min)
   - ContextSelectionProcessor selects best documents fitting in 600 tokens
   - Might end up with 3-4 high-quality documents depending on their length
```

### 5. Adaptive Quality Features

The system includes several adaptive features for optimal performance:

- **Zero-Context Handling**: If no tokens are available for context, gracefully continues with just the conversation
- **Large Document Handling**: Automatically skips documents that are too large for the token budget
- **Relevance Filtering**: Similarity threshold prevents inclusion of irrelevant documents
- **Logging and Monitoring**: Detailed logs show selection decisions for debugging

This approach ensures that RAG execution is both intelligent and efficient, maximizing the quality of retrieved context while respecting all token constraints and user preferences.

## Key Benefits

1. **Intelligent Request Routing**: Automatically determines when to use RAG vs. direct LLM calls based on content type, tools, and message structure
2. **Smart Message Processing**: Extracts recent user queries for vector search while preserving full conversation context
3. **Adaptive Token Management**: Dynamic context allocation with configurable ratios and automatic `max_tokens` adjustment
4. **Precision Document Selection**: Multi-layered filtering combining relevance scoring, similarity thresholds, and token budgets
5. **Graceful Degradation**: Handles edge cases (no context space, oversized documents) without failure

This algorithm maximizes RAG effectiveness while maintaining OpenAI API compatibility and robust error handling.