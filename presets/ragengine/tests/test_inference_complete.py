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

import asyncio
from unittest.mock import AsyncMock, patch

import pytest
from fastapi import HTTPException
from llama_index.core.llms import CompletionResponse

from ragengine.inference.inference import Inference


def test_complete_returns_acomplete_result():
    llm = Inference()
    with patch.object(
        Inference,
        "_async_completions",
        new=AsyncMock(return_value=CompletionResponse(text="hello")),
    ):
        assert llm.complete("hi").text == "hello"


def test_complete_passes_through_empty_response():
    # "Empty Response" used to be turned into a 422 for LLMRerank, which is no
    # longer used. complete() should hand back whatever the LLM returned.
    llm = Inference()
    with patch.object(
        Inference,
        "_async_completions",
        new=AsyncMock(return_value=CompletionResponse(text="Empty Response")),
    ):
        assert llm.complete("hi").text == "Empty Response"


def test_complete_works_inside_running_event_loop():
    llm = Inference()

    async def caller():
        return llm.complete("hi")

    with patch.object(
        Inference,
        "_async_completions",
        new=AsyncMock(return_value=CompletionResponse(text="ok")),
    ):
        assert asyncio.run(caller()).text == "ok"


def test_complete_wraps_unexpected_errors():
    llm = Inference()
    with (
        patch.object(
            Inference,
            "_async_completions",
            new=AsyncMock(side_effect=RuntimeError("boom")),
        ),
        pytest.raises(HTTPException) as exc_info,
    ):
        llm.complete("hi")
    assert exc_info.value.status_code == 500
