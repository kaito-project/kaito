---
title: RAG Output Guardrails
---

This page describes the initial output guardrails hook added to RAGEngine chat completions.

## Scope

This document reflects the current non-streaming implementation only.

At this stage, the feature is limited to:

- assistant text returned by `/v1/chat/completions`
- non-streaming responses
- runtime configuration loaded from environment variables inside the RAG service

It does not yet describe a full CRD-backed end-user configuration flow.

## What The Hook Does

RAGEngine can scan assistant output before returning the final response to the caller.

If configured checks are triggered, the response can be handled in one of two ways:

- `redact`: replace matching content with sanitized output
- `block`: replace the assistant message with a fixed block message

## Current Runtime Configuration

The current implementation reads these environment variables at runtime:

- `OUTPUT_GUARDRAILS_ENABLED`
- `OUTPUT_GUARDRAILS_ACTION_ON_HIT`
- `OUTPUT_GUARDRAILS_REGEX_PATTERNS`
- `OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS`
- `OUTPUT_GUARDRAILS_BLOCK_MESSAGE`

These variables are consumed by the RAGEngine process when it starts.

## Current Limitations

- This page documents the initial hook only.
- It does not imply that a standard KAITO `RAGEngine` spec field is already available for end users.
- Streaming response scanning is not included in this stage.
- Auditing and richer operational controls are not included in this stage.

## Related Docs

- See [RAG API](./rag-api.md) for the chat completions endpoint.
- See [Retrieval-Augmented Generation (RAG)](./rag.md) for the broader RAGEngine overview.