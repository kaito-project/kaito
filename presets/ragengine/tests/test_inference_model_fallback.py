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

import httpx
import pytest
import requests

from ragengine.inference.inference import Inference

LLM_URL = "http://llm.example/v1/completions"


def _status_error(status_code: int) -> httpx.HTTPStatusError:
    request = httpx.Request("POST", LLM_URL)
    response = httpx.Response(status_code, request=request)
    return httpx.HTTPStatusError(
        f"HTTP {status_code}", request=request, response=response
    )


def _ok_response(text: str = "answer") -> dict:
    return {"choices": [{"text": text}]}


def test_fetch_default_model_info_returns_pair_when_models_endpoint_fails():
    llm = Inference()
    with patch(
        "ragengine.inference.inference.requests.get",
        side_effect=requests.ConnectionError("models endpoint is down"),
    ):
        assert llm._fetch_default_model_info() == (None, None)


def test_get_default_model_info_survives_unreachable_models_endpoint():
    llm = Inference()
    with patch(
        "ragengine.inference.inference.requests.get",
        side_effect=requests.ConnectionError("models endpoint is down"),
    ):
        assert llm._get_default_model_info() == (None, None)


def test_completions_retry_with_fetched_model_after_400_without_model():
    llm = Inference()
    payloads = []

    # The request dict is reused for the retry, so snapshot it on every call.
    async def post(self, data, headers):
        payloads.append(dict(data))
        if len(payloads) == 1:
            raise _status_error(400)
        return _ok_response("retried")

    with (
        patch.object(Inference, "_get_default_model_info", return_value=(None, None)),
        patch.object(
            Inference, "_fetch_default_model_info", return_value=("my-model", 4096)
        ),
        patch.object(Inference, "_async_post_request_raw", new=post),
    ):
        result = asyncio.run(llm._async_completions("hi"))

    assert result.text == "retried"
    assert len(payloads) == 2
    assert "model" not in payloads[0]
    assert payloads[1]["model"] == "my-model"


def test_completions_retry_aborts_with_original_error_when_no_model_found():
    llm = Inference()
    post = AsyncMock(side_effect=_status_error(400))
    with (
        patch.object(Inference, "_get_default_model_info", return_value=(None, None)),
        patch(
            "ragengine.inference.inference.requests.get",
            side_effect=requests.ConnectionError("models endpoint is down"),
        ),
        patch.object(Inference, "_async_post_request_raw", new=post),
        pytest.raises(httpx.HTTPStatusError) as exc_info,
    ):
        asyncio.run(llm._async_completions("hi"))

    assert exc_info.value.response.status_code == 400
    assert post.await_count == 1


@pytest.mark.parametrize("status_code", [401, 404, 500, 503])
def test_completions_do_not_retry_on_other_http_errors(status_code):
    llm = Inference()
    post = AsyncMock(side_effect=_status_error(status_code))
    fetch = patch.object(
        Inference, "_fetch_default_model_info", return_value=("my-model", 4096)
    )
    with (
        patch.object(Inference, "_get_default_model_info", return_value=(None, None)),
        fetch as fetch_mock,
        patch.object(Inference, "_async_post_request_raw", new=post),
        pytest.raises(httpx.HTTPStatusError) as exc_info,
    ):
        asyncio.run(llm._async_completions("hi"))

    assert exc_info.value.response.status_code == status_code
    assert post.await_count == 1
    fetch_mock.assert_not_called()


def test_completions_do_not_retry_when_model_is_already_known():
    llm = Inference()
    post = AsyncMock(side_effect=_status_error(400))
    with (
        patch.object(
            Inference, "_get_default_model_info", return_value=("known", 4096)
        ),
        patch.object(
            Inference, "_fetch_default_model_info", return_value=("other", 4096)
        ) as fetch_mock,
        patch.object(Inference, "_async_post_request_raw", new=post),
        pytest.raises(httpx.HTTPStatusError),
    ):
        asyncio.run(llm._async_completions("hi"))

    assert post.await_count == 1
    fetch_mock.assert_not_called()
