# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

import json

from ragengine.streaming.downstream import (
    build_block_sse_chunk,
    build_sse_data_chunk,
    build_sse_done_chunk,
)
from ragengine.streaming.upstream import (
    extract_delta_content,
    is_sse_done_event,
    parse_sse_data_line,
)


def test_parse_sse_data_line_extracts_payload():
    assert parse_sse_data_line('data: {"hello":"world"}') == '{"hello":"world"}'


def test_parse_sse_data_line_ignores_non_data_lines():
    assert parse_sse_data_line("event: message") is None


def test_is_sse_done_event_detects_done_marker():
    assert is_sse_done_event("[DONE]") is True
    assert is_sse_done_event('{"choices":[]}') is False


def test_extract_delta_content_returns_first_choice_content():
    payload = {"choices": [{"delta": {"content": "Hello"}}]}

    assert extract_delta_content(payload) == "Hello"


def test_build_sse_data_chunk_builds_openai_compatible_frame():
    payload = {"choices": [{"delta": {"content": "Hello"}}]}

    frame = build_sse_data_chunk(payload)

    assert frame.endswith("\n\n")
    parsed_payload = parse_sse_data_line(frame.strip())
    assert parsed_payload is not None
    assert json.loads(parsed_payload) == payload


def test_build_sse_done_chunk_builds_done_frame():
    assert build_sse_done_chunk() == "data: [DONE]\n\n"


def test_build_block_sse_chunk_builds_content_filter_frame():
    frame = build_block_sse_chunk("blocked")

    parsed_payload = parse_sse_data_line(frame.strip())
    assert parsed_payload is not None
    payload = json.loads(parsed_payload)
    assert payload["choices"][0]["delta"]["content"] == "blocked"
    assert payload["choices"][0]["finish_reason"] == "content_filter"