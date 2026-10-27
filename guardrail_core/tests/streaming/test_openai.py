# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import copy
import json
import sys

import pytest
from guardrail_core.streaming.openai import (
    OpenAIChatChunkParseStatus as Status,
)
from guardrail_core.streaming.openai import (
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_data_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from guardrail_core.streaming.sse import SSEEvent, SSEFramer


def parse_payload(payload):
    return parse_openai_chat_sse_event(
        SSEEvent.from_raw("data: " + json.dumps(payload))
    )


@pytest.mark.parametrize(
    "body",
    (
        "",
        ": comment",
        "event: message\nid: 1",
        "data",
        "data:",
        "data: ",
        "data: \t",
        "[DONE]",
    ),
)
def test_no_data(body):
    result = parse_openai_chat_sse_event(SSEEvent.from_raw(body))
    assert result.status == Status.NO_DATA
    assert result.payload is None
    assert result.parsed_choices == ()


@pytest.mark.parametrize(
    "body",
    (
        "data:[DONE]",
        "data: [DONE]",
        "data:  [DONE] \t",
        ": comment\r\nevent: message\r\nid: 1\r\ndata:[DONE]",
    ),
)
def test_done(body):
    result = parse_openai_chat_sse_event(SSEEvent.from_raw(body))
    assert result.status == Status.DONE
    assert result.payload is None


@pytest.mark.parametrize("data", ("{invalid json}", '{"choices": [}', "not JSON"))
def test_malformed_json_is_distinct(data):
    result = parse_openai_chat_sse_event(SSEEvent.from_raw(f"data:{data}"))
    assert result.status == Status.MALFORMED_JSON
    assert result.error


def test_oversized_integer_is_malformed_json():
    if not hasattr(sys, "set_int_max_str_digits"):
        pytest.skip("Python does not enforce a maximum integer string length.")
    original_limit = sys.get_int_max_str_digits()
    sys.set_int_max_str_digits(4300)
    data = '{"choices":[],"x":' + "1" * 5000 + "}"
    try:
        result = parse_openai_chat_sse_event(SSEEvent.from_raw(f"data:{data}"))
    finally:
        sys.set_int_max_str_digits(original_limit)
    assert result.status == Status.MALFORMED_JSON
    assert result.error


def test_decoder_recursion_error_is_malformed_json(monkeypatch):
    def raise_recursion_error(_: str):
        raise RecursionError("maximum recursion depth exceeded")

    monkeypatch.setattr(
        "guardrail_core.streaming.openai.json.loads", raise_recursion_error
    )

    result = parse_openai_chat_sse_event(SSEEvent.from_raw('data:{"choices":[]}'))
    assert result.status == Status.MALFORMED_JSON
    assert result.error


@pytest.mark.parametrize(
    "payload",
    (
        None,
        [],
        "text",
        42,
        True,
        {"choices": None},
        {"choices": {}},
        {"choices": "text"},
        {"choices": [None]},
        {"choices": [[]]},
        {"choices": ["text"]},
    ),
)
def test_invalid_payload_shape(payload):
    result = parse_payload(payload)
    assert result.status == Status.INVALID_PAYLOAD
    assert result.error
    if isinstance(payload, dict):
        assert result.payload == payload


@pytest.mark.parametrize("index", (None, "0", False, True, 0.0, [], {}))
def test_index_must_be_integer_and_not_bool(index):
    result = parse_payload({"choices": [{"index": index, "delta": {}}]})
    assert result.status == Status.INVALID_PAYLOAD
    assert "index" in result.error


def test_index_is_required():
    assert parse_payload({"choices": [{"delta": {}}]}).status == Status.INVALID_PAYLOAD


@pytest.mark.parametrize("index", (0, 1, -1, 100))
@pytest.mark.parametrize(
    "delta_fields",
    (
        {},
        {"delta": None},
        {"delta": {}},
        {"delta": {"role": "assistant"}},
        {"delta": {"content": None}},
        {"delta": {"tool_calls": []}},
    ),
)
def test_missing_null_or_non_text_delta_preserves_choice(index, delta_fields):
    choice = {"index": index, **delta_fields}
    result = parse_payload({"choices": [choice]})
    assert result.status == Status.PARSED
    assert len(result.parsed_choices) == 1
    parsed = result.parsed_choices[0]
    assert parsed.choice_index == index
    assert parsed.content is None
    assert parsed.finish_reason is None
    assert parsed.choice == choice
    assert result.contents == ()


@pytest.mark.parametrize("delta", ([], "", "text", 0, False))
def test_invalid_delta(delta):
    result = parse_payload({"choices": [{"index": 0, "delta": delta}]})
    assert result.status == Status.INVALID_PAYLOAD
    assert "delta" in result.error


@pytest.mark.parametrize("value", ([], {}, 0, False, True))
@pytest.mark.parametrize("field", ("content", "finish_reason"))
def test_non_string_non_null_text_fields_are_invalid(field, value):
    choice = {"index": 0, "delta": {}}
    if field == "content":
        choice["delta"]["content"] = value
    else:
        choice["finish_reason"] = value
    result = parse_payload({"choices": [choice]})
    assert result.status == Status.INVALID_PAYLOAD
    assert field in result.error


def test_multiple_choices_preserve_role_tools_payload_and_alignment():
    payload = {
        "id": "chat-123",
        "object": "chat.completion.chunk",
        "model": "example",
        "usage": {"total_tokens": 10},
        "choices": [
            {"index": 3, "delta": {"role": "assistant"}, "extra": "preserved"},
            {"index": 0, "delta": {"content": "first"}},
            {"index": 2, "delta": {"content": ""}},
            {"index": 7, "delta": {"tool_calls": [{"id": "call-1"}]}},
            {"index": 1, "delta": {}, "finish_reason": "stop"},
        ],
    }
    original = copy.deepcopy(payload)
    result = parse_payload(payload)
    assert result.status == Status.PARSED
    assert result.payload == original
    assert [choice.choice_index for choice in result.parsed_choices] == [3, 0, 2, 7, 1]
    assert [choice.choice for choice in result.parsed_choices] == original["choices"]
    assert result.contents == ("first", "")
    assert result.finish_reasons == ("stop",)
    assert payload == original


def test_invalid_later_choice_does_not_produce_partial_success():
    payload = {"choices": [{"index": 0, "delta": {"content": "valid"}}, None]}
    result = parse_payload(payload)
    assert result.status == Status.INVALID_PAYLOAD
    assert result.parsed_choices == ()
    assert result.contents == ()
    assert result.payload == payload


@pytest.mark.parametrize(
    "payload", ({}, {"choices": []}, {"usage": {"total_tokens": 10}})
)
def test_empty_or_missing_choices(payload):
    result = parse_payload(payload)
    assert result.status == Status.PARSED
    assert result.payload == payload
    assert result.parsed_choices == ()


def test_multiline_json_data():
    body = ': comment\rdata: {\ndata:"choices": [{"index": 2, "delta": {"content": "hi"}}]\r\ndata: }'
    result = parse_openai_chat_sse_event(SSEEvent.from_raw(body))
    assert result.status == Status.PARSED
    assert result.contents == ("hi",)


def test_fragmented_streaming_sequence():
    chunks = [
        b'data: {"choices":[{"index":0,"delta":{"content":"hello',
        b' world"}}]}\n\ndata: {"choices":[{"index":2,"delta":{"content":',
        b' "!"}}]}\n\ndata: [DONE]\n\n',
    ]
    framer = SSEFramer()
    results = []
    for chunk in chunks:
        results.extend(
            parse_openai_chat_sse_event(event) for event in framer.feed(chunk)
        )
    results.extend(parse_openai_chat_sse_event(event) for event in framer.finish())
    assert [result.status for result in results] == [
        Status.PARSED,
        Status.PARSED,
        Status.DONE,
    ]
    assert [result.contents for result in results] == [("hello world",), ("!",), ()]


@pytest.mark.parametrize(
    "content", ("safe text", "", "\u4f60\u597d", "line\nnext\r\nend")
)
def test_delta_builder_round_trip(content):
    wire = build_openai_chat_delta_sse_chunk(content, choice_index=3)
    framer = SSEFramer()
    events = framer.feed(wire.encode()) + framer.finish()
    assert len(events) == 1
    result = parse_openai_chat_sse_event(events[0])
    assert result.contents == (content,)
    assert result.parsed_choices[0].choice_index == 3


@pytest.mark.parametrize("reason", ("stop", "length", "content_filter"))
def test_finish_builder_round_trip(reason):
    wire = build_openai_chat_finish_reason_sse_chunk(
        finish_reason=reason, choice_index=2
    )
    result = parse_openai_chat_sse_event(SSEFramer().feed(wire.encode())[0])
    assert result.finish_reasons == (reason,)
    assert result.parsed_choices[0].choice_index == 2


def test_default_finish_builder_and_done():
    result = parse_openai_chat_sse_event(
        SSEFramer().feed(build_openai_chat_finish_reason_sse_chunk().encode())[0]
    )
    assert result.finish_reasons == ("content_filter",)
    assert build_sse_done_chunk() == "data: [DONE]\n\n"
    result = parse_openai_chat_sse_event(
        SSEFramer().feed(build_sse_done_chunk().encode())[0]
    )
    assert result.status == Status.DONE


def test_generic_builder_preserves_payload():
    payload = {
        "id": "example",
        "choices": [
            {"index": 0, "delta": {"role": "assistant"}},
            {"index": 1, "delta": {"content": "hi"}, "logprobs": {"content": []}},
        ],
        "usage": {"total_tokens": 10},
    }
    wire = build_sse_data_chunk(payload)
    result = parse_openai_chat_sse_event(SSEFramer().feed(wire.encode())[0])
    assert result.payload == payload
    assert build_sse_data_chunk(result.payload) == wire
