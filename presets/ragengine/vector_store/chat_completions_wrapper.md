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

After message processing, the algorithm validates token limits:

1. **Context Window Check**: Total prompt length must not exceed model's context window
2. **Max Tokens Adjustment**: If `max_tokens` + prompt length exceeds context window, `max_tokens` is automatically reduced

```python
if prompt_len > self.llm.metadata.context_window:
    # Error: Prompt too long
    
if max_tokens > context_window - prompt_len:
    # Automatically adjust max_tokens
    max_tokens = context_window - prompt_len
```

## RAG Execution

Finally, for valid RAG requests:

1. **Dynamic top_k calculation**: Based on available context window space
2. **ContextSelectionProcessor**: Intelligently selects relevant document nodes
3. **Chat engine execution**: Uses user_prompt for vector search, chat_history for context

```python
# Calculate retrieval size based on available tokens
top_k = max(100, int((context_window - prompt_len) / 500))

# Execute with intelligent context selection
chat_result = await chat_engine.achat(user_prompt, chat_history=chat_history)
```

## Key Benefits

1. **Smart Bypass Logic**: Automatically handles tool calls, multi-modal content, and other non-RAG scenarios
2. **Optimal Vector Search**: Only searches on relevant user messages, not entire conversation
3. **Context Preservation**: Maintains full conversation history for LLM context
4. **Token Management**: Automatic adjustment to prevent context window overflow
5. **Error Handling**: Clear validation and error messages for invalid requests

This algorithm ensures that RAG is only used when appropriate while maintaining the full conversational context and optimizing for both relevance and performance.