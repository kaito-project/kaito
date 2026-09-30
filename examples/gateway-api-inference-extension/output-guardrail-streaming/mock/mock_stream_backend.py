#!/usr/bin/env python3
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

"""Mock OpenAI-compatible streaming chat completions backend.

Listens on :8080 and answers ``POST /v1/chat/completions`` with a Server-Sent Events stream of ``chat.completion.chunk`` events. The response is written in small pieces (one write per event by default) so Envoy forwards each piece as a separate ext_proc ``response_body`` message.

Scenario selection: ``?scenario=<name>[&delay_ms=<ms>][&split=<n>]``.

Scenarios (driven by issue #2358 experiments):

* ``normal``  -- several clean events plus ``[DONE]``.
* ``aligned`` -- one event whose text contains the banned word ``foo`` (caught by a within-event scan).
* ``split``   -- a single long event written in several chunks so Envoy and ext_proc see it fragmented; the guard must reassemble it.
* ``cross``   -- event N ends with ``sk-`` and event N+1 starts with the rest of the secret, so the secret spans two SSE events. A holdback of at least the secret length redacts it; without one the halves can be flushed separately and leak.
* ``slow``    -- ``normal`` with an inter-write delay to measure TTFT/latency.
* ``block``   -- an event containing the banned word ``PROHIBITED`` that the policy turns into a hard block for the remaining stream; already-released content may remain.
"""

import http.server
import json
import time
import urllib.parse

HOST = "0.0.0.0"
PORT = 8080

DONE_FRAME = b"data: [DONE]\n\n"

CROSS_SECRET = "sk-1234567890abcdef"


def _chunk(content: str, finish_reason: str | None = None) -> str:
    payload = {
        "id": "chatcmpl-mock",
        "model": "mock",
        "object": "chat.completion.chunk",
        "created": 0,
        "choices": [
            {
                "index": 0,
                "delta": {"content": content},
                "finish_reason": finish_reason,
            }
        ],
    }
    return "data: " + json.dumps(payload, ensure_ascii=False) + "\n\n"


def _normal_events() -> list[tuple[str, str | None]]:
    return [
        ("The round trip takes ", None),
        ("roughly a second to ", None),
        ("stream back the answer.", None),
    ]


def _cross_events() -> list[tuple[str, str | None]]:
    return [
        ("The round trip takes ", None),
        ("roughly a second and the key is " + CROSS_SECRET[:3], None),
        (CROSS_SECRET[3:] + " and that is all.", None),
        (" Hope it helps.", None),
    ]


def _scenario_events(scenario: str) -> list[tuple[str, str | None]]:
    if scenario == "aligned":
        return [("contains foo inside the text", None)]
    if scenario == "split":
        return [("reassembled across many writes", None)]
    if scenario == "cross":
        return _cross_events()
    if scenario == "block":
        return [("here is the PROHIBITED phrase", None)]
    return _normal_events()


class MockHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self) -> None:
        parsed = urllib.parse.urlsplit(self.path)
        query = urllib.parse.parse_qs(parsed.query)
        scenario = (query.get("scenario") or ["normal"])[0]
        delay_ms = float((query.get("delay_ms") or ["0"])[0])
        self._stream(scenario, delay_ms)

    def _stream(self, scenario: str, delay_ms: float) -> None:
        events = _scenario_events(scenario)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()

        for index, (content, finish_reason) in enumerate(events):
            frame = _chunk(content, finish_reason)
            try:
                self._write_chunk(frame)
                if delay_ms > 0:
                    time.sleep(delay_ms / 1000.0)
            except (BrokenPipeError, ConnectionResetError):
                return
        try:
            self._write_chunk(DONE_FRAME)
        except (BrokenPipeError, ConnectionResetError):
            return
        self.wfile.write(b"0\r\n\r\n")
        self.wfile.flush()

    def _write_chunk(self, payload: str | bytes) -> None:
        raw = payload.encode("utf-8") if isinstance(payload, str) else payload
        self.wfile.write(f"{len(raw):X}\r\n".encode("ascii"))
        self.wfile.write(raw)
        self.wfile.write(b"\r\n")
        self.wfile.flush()

    def log_message(self, fmt: str, *args) -> None:
        pass


def main() -> None:
    server = http.server.ThreadingHTTPServer((HOST, PORT), MockHandler)
    print(f"mock-stream-backend listening on {HOST}:{PORT}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
