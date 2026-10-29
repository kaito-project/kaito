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

import os
import sys

from guardrail_core.streaming import window

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.streaming import buffer_window, guardrails  # noqa: E402


def test_window_compatibility_exports_are_shared_types():
    for name in (
        "StreamingBufferWindow",
        "WindowEmitResult",
        "WindowScanError",
        "WindowScanner",
        "WindowScanResult",
    ):
        assert getattr(buffer_window, name) is getattr(window, name)


def test_streaming_guardrails_use_shared_window():
    assert guardrails.StreamingBufferWindow is window.StreamingBufferWindow
    assert guardrails.WindowScanResult is window.WindowScanResult
