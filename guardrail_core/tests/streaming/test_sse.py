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

import pytest
from guardrail_core.streaming.sse import (
    SSEDecodeError,
    SSEEvent,
    SSEEventTooLargeError,
    SSEFramer,
    SSEIncompleteEventError,
)

ENDINGS = (b"\n\n", b"\r\n\r\n", b"\r\r", b"\n\r", b"\r\n\r", b"\n\r\n", b"\r\n\n")


@pytest.mark.parametrize("ending", ENDINGS)
@pytest.mark.parametrize("partial", (b"", b"data: unfinished"))
def test_every_chunk_boundary_and_eof(ending, partial):
    first = b'data: {"choices":[]}'
    second = b"data:[DONE]"
    raw = first + ending + second + ending + partial
    for offset in range(len(raw) + 1):
        framer = SSEFramer()
        events = framer.feed(raw[:offset]) + framer.feed(raw[offset:])
        if partial:
            with pytest.raises(SSEIncompleteEventError) as caught:
                framer.finish()
            assert caught.value.raw_bytes == partial
        else:
            events.extend(framer.finish())
        assert [event.raw for event in events] == [
            first.decode(),
            second.decode(),
        ], offset
        assert (
            b"".join(event.raw_bytes for event in events)
            == raw[: len(raw) - len(partial)]
        )


@pytest.mark.parametrize("ending", ENDINGS)
def test_byte_at_a_time_and_utf8(ending):
    body = "data: \u4f60\u597d \U0001f642".encode()
    wire = body + ending + b"data:[DONE]" + ending
    framer = SSEFramer()
    events = []
    for byte in wire:
        events.extend(framer.feed(bytes([byte])))
    events.extend(framer.finish())
    assert [event.data for event in events] == [
        "\u4f60\u597d \U0001f642",
        "[DONE]",
    ]
    assert b"".join(event.raw_bytes for event in events) == wire


def test_initial_utf8_bom_is_ignored_in_parsed_view():
    wire = b'\xef\xbb\xbfdata: {"choices":[]}\n\n'
    event = SSEFramer().feed(wire)[0]
    assert event.raw == 'data: {"choices":[]}'
    assert event.data == '{"choices":[]}'
    assert event.raw_bytes == wire


def test_initial_utf8_bom_split_across_chunks_is_ignored():
    wire = b"\xef\xbb\xbfdata: value\n\n"
    framer = SSEFramer()
    assert framer.feed(wire[:1]) == []
    assert framer.feed(wire[1:2]) == []
    event = framer.feed(wire[2:])[0]
    assert event.data == "value"
    assert event.raw_bytes == wire


def test_utf8_bom_after_first_event_is_not_ignored():
    wire = b": comment\n\n\xef\xbb\xbfdata: value\n\n"
    events = SSEFramer().feed(wire)
    assert [event.data for event in events] == [None, None]
    assert events[1].raw == "\ufeffdata: value"
    assert b"".join(event.raw_bytes for event in events) == wire


def test_all_partitions_of_utf8_crlf_event():
    wire = b"data:\xc3\xa9\r\n\n"
    for mask in range(1 << (len(wire) - 1)):
        framer = SSEFramer()
        events = []
        start = 0
        for index in range(1, len(wire)):
            if mask & (1 << (index - 1)):
                events.extend(framer.feed(wire[start:index]))
                start = index
        events.extend(framer.feed(wire[start:]))
        events.extend(framer.finish())
        assert len(events) == 1
        assert events[0].data == "\u00e9"
        assert events[0].raw_bytes == wire


@pytest.mark.parametrize("newline", (b"\n", b"\r\n", b"\r"))
def test_fields_comments_multiline_and_lossless_bytes(newline):
    lines = (
        b": keepalive",
        b"event: message",
        b"id: 12",
        b"retry: 1000",
        b"data: {",
        b'data:"choices": []',
        b"data: }",
    )
    body = newline.join(lines)
    wire = body + newline * 2
    framer = SSEFramer()
    events = framer.feed(wire) + framer.finish()
    assert len(events) == 1
    event = events[0]
    assert event.raw_bytes == wire
    assert event.raw == body.decode()
    assert event.lines == tuple(line.decode() for line in lines)
    assert event.data == '{\n"choices": []\n}'


@pytest.mark.parametrize(
    ("body", "data"),
    (
        ("", None),
        (": keepalive", None),
        ("event: message\nid: 1", None),
        ("data", ""),
        ("data:", ""),
        ("data: ", ""),
        ("data:  value", " value"),
        ("data:one\ndata\ndata:three", "one\n\nthree"),
        ("database: ignored\nData: ignored", None),
        ("data: a\u2028b\u0085c", "a\u2028b\u0085c"),
    ),
)
def test_data_field_rules(body, data):
    assert SSEEvent.from_raw(body).data == data


def test_blank_and_comment_only_events_are_preserved():
    framer = SSEFramer()
    wire = b"\n\r\n: comment\r\r"
    events = framer.feed(wire) + framer.finish()
    assert [event.raw for event in events] == ["", "", ": comment"]
    assert [event.data for event in events] == [None, None, None]
    assert b"".join(event.raw_bytes for event in events) == wire


def test_trailing_cr_waits_for_next_chunk():
    framer = SSEFramer()
    assert framer.feed(b"data:[DONE]\r\n\r") == []
    assert framer.feed(b"") == []
    events = framer.feed(b"\n")
    assert events[0].raw_bytes == b"data:[DONE]\r\n\r\n"
    assert framer.finish() == []


@pytest.mark.parametrize("ending", (b"\r\r", b"\n\r", b"\r\n\r"))
def test_eof_resolves_trailing_cr(ending):
    framer = SSEFramer()
    assert framer.feed(b"data:[DONE]" + ending) == []
    assert framer.finish()[0].data == "[DONE]"


@pytest.mark.parametrize(
    "raw",
    (
        b"data:[DONE]",
        b"data:[DONE]\n",
        b"data:[DONE]\r",
        b"data:[DONE]\r\n",
        b'data: {"choices":',
        b"data:\xe4\xbd",
    ),
)
def test_eof_reports_incomplete_event_without_fabricating_delimiter(raw):
    framer = SSEFramer()
    assert framer.feed(raw) == []
    with pytest.raises(SSEIncompleteEventError) as caught:
        framer.finish()
    assert caught.value.raw_bytes == raw
    assert caught.value.completed_events == ()


def test_empty_stream():
    framer = SSEFramer()
    assert framer.feed(b"") == []
    assert framer.finish() == []


def test_invalid_utf8_is_not_decoded_until_event_is_complete():
    framer = SSEFramer()
    assert framer.feed(b"data:\xff") == []
    with pytest.raises(SSEDecodeError) as caught:
        framer.feed(b"\n\n")
    assert caught.value.raw_bytes == b"data:\xff\n\n"
    assert isinstance(caught.value.__cause__, UnicodeDecodeError)


def test_error_retains_prior_events_in_same_call():
    framer = SSEFramer()
    with pytest.raises(SSEDecodeError) as caught:
        framer.feed(b"data: valid\n\ndata:\xff\n\n")
    assert [event.data for event in caught.value.completed_events] == ["valid"]


@pytest.mark.parametrize("ending", ENDINGS)
def test_size_limit_counts_wire_bytes_and_resets_per_event(ending):
    wire = b"data:ok" + ending
    framer = SSEFramer(max_event_bytes=len(wire))
    events = framer.feed(wire * 3) + framer.finish()
    assert len(events) == 3
    too_small = SSEFramer(max_event_bytes=len(wire) - 1)
    with pytest.raises(SSEEventTooLargeError):
        too_small.feed(wire)


@pytest.mark.parametrize("fragmented", (True, False))
def test_unterminated_event_cannot_accumulate_without_bound(fragmented):
    framer = SSEFramer(max_event_bytes=8)
    with pytest.raises(SSEEventTooLargeError) as caught:
        if fragmented:
            for _ in range(9):
                framer.feed(b"x")
        else:
            framer.feed(b"x" * 1_000_000)
    assert caught.value.raw_bytes == b"x" * 8


def test_size_error_retains_completed_events():
    framer = SSEFramer(max_event_bytes=8)
    with pytest.raises(SSEEventTooLargeError) as caught:
        framer.feed(b"data:a\n\n" + b"x" * 9)
    assert [event.data for event in caught.value.completed_events] == ["a"]


@pytest.mark.parametrize("limit", (0, -1, True, None, "8", 1.5))
def test_invalid_limit_is_explicit(limit):
    with pytest.raises((ValueError, TypeError), match="max_event_bytes"):
        SSEFramer(max_event_bytes=limit)


def test_non_bytes_input_is_explicit():
    framer = SSEFramer()
    with pytest.raises(TypeError, match="bytes"):
        framer.feed("data: no\n\n")
    assert framer.feed(b"") == []


@pytest.mark.parametrize("failure", (False, True))
def test_framer_cannot_be_reused_after_finish_or_error(failure):
    framer = SSEFramer()
    if failure:
        with pytest.raises(SSEDecodeError):
            framer.feed(b"\xff\n\n")
    else:
        framer.finish()
    with pytest.raises(ValueError, match="closed"):
        framer.feed(b"data: late\n\n")
    with pytest.raises(ValueError, match="closed"):
        framer.finish()
