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
import concurrent.futures
import json
import logging
from collections.abc import Sequence
from typing import Any
from urllib.parse import urljoin, urlparse

import httpx
import requests
from fastapi import HTTPException
from llama_index.core.llms import (
    ChatMessage,
    ChatResponse,
    CompletionResponse,
    CompletionResponseGen,
    CustomLLM,
    LLMMetadata,
)
from llama_index.core.llms.callbacks import llm_chat_callback, llm_completion_callback
from openai.types.chat import (
    CompletionCreateParams,
)
from pydantic import PrivateAttr
from requests.exceptions import HTTPError

from ragengine.config import (
    LLM_ACCESS_SECRET,
    LLM_CONTEXT_WINDOW,
    LLM_INFERENCE_URL,
)
from ragengine.models import ChatCompletionResponse

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

OPENAI_URL_PREFIX = "https://api.openai.com"
HUGGINGFACE_URL_PREFIX = "https://api-inference.huggingface.co"
DEFAULT_HEADERS = {
    "Authorization": f"Bearer {LLM_ACCESS_SECRET}",
    "Content-Type": "application/json",
}
DEFAULT_HTTP_TIMEOUT = 300.0  # Seconds
DEFAULT_HTTP_SUCCESS_CODE = 200


class Inference(CustomLLM):
    params: dict = {}
    _default_model: str = None
    _default_max_model_len: int = None
    _model_retrieval_attempted: bool = False
    _async_http_client: httpx.AsyncClient = PrivateAttr(default=None)

    async def _get_httpx_client(self):
        """Lazily initializes the HTTP client on first request."""
        if self._async_http_client is None:
            self._async_http_client = httpx.AsyncClient(timeout=DEFAULT_HTTP_TIMEOUT)
        return self._async_http_client

    def set_params(self, params: dict) -> None:
        self.params = params

    def get_param(self, key, default=None):
        return self.params.get(key, default)

    @llm_completion_callback()
    def stream_complete(self, prompt: str, **kwargs: Any) -> CompletionResponseGen:
        pass

    def run_async_coroutine(self, coro):
        with concurrent.futures.ThreadPoolExecutor() as executor:
            # Submit a task that creates its own event loop using asyncio.run
            future = executor.submit(asyncio.run, coro)
            return future.result()

    @llm_completion_callback()
    def complete(
        self, prompt: str, formatted: bool = False, **kwargs: Any
    ) -> CompletionResponse:
        # Required implementation - Only called by LlamaIndex reranker because LLMRerank library doesn't use async call
        try:
            result = self.run_async_coroutine(
                self.acomplete(prompt, formatted=formatted, **kwargs)
            )
            if result.text == "Empty Response":
                logger.error(
                    "LLMRerank Request returned an unparsable or invalid response"
                )
                raise HTTPException(
                    status_code=422,
                    detail="Rerank operation failed: Invalid response from LLM. This feature is experimental.",
                )
            return result
        except HTTPException as http_exc:
            # If it's already an HTTPException (e.g., 422), re-raise it as is
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in complete(): {e}")
            raise HTTPException(
                status_code=500, detail=f"An unexpected error occurred: {str(e)}"
            )

    @llm_completion_callback()
    async def acomplete(
        self, prompt: str, formatted: bool = False, **kwargs: Any
    ) -> CompletionResponse:
        try:
            return await self._async_completions(prompt, **kwargs, **self.params)
        except HTTPException as http_exc:
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in acomplete(): {e}")
            raise HTTPException(
                status_code=500, detail=f"An unexpected error occurred: {str(e)}"
            )
        finally:
            # Clear params after the completion is done
            self.params = {}

    @llm_chat_callback()
    def chat(
        self,
        messages: Sequence[ChatMessage],
        **kwargs: Any,
    ) -> ChatResponse:
        """Perform a chat completion request."""
        try:
            logger.info(
                f"Sending chat request to {LLM_INFERENCE_URL} with messages: {messages}, and args: {kwargs}"
            )
            return self.run_async_coroutine(self.achat(messages, **kwargs))
        except HTTPException as http_exc:
            logger.error(f"HTTP exception during chat(): {http_exc.detail}")
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in chat(): {e}")
            raise HTTPException(
                status_code=500, detail=f"An unexpected error occurred: {str(e)}"
            )

    @llm_chat_callback()
    async def achat(
        self,
        messages: Sequence[ChatMessage],
        **kwargs: Any,
    ) -> ChatResponse:
        """Perform an asynchronous chat completion request."""
        try:
            base_model, base_max_len = self._get_default_model_info()
            req = {
                "model": self.get_param("model", base_model),
                "max_tokens": self.get_param("max_tokens", base_max_len),
                "messages": [
                    {
                        "role": message.role,
                        "content": message.content
                        if isinstance(message.content, str)
                        else json.dumps(message.content),
                    }
                    for message in messages
                    if message.content is not None and message.content != ""
                ],
            }

            # Add any additional parameters from self.params onto request
            for key, value in self.params.items():
                if key not in req:
                    req[key] = value

            resp = await self._async_post_request_raw(data=req, headers=DEFAULT_HEADERS)
            return ChatResponse(
                logprobs=resp.get("logprobs", None),
                delta=resp.get("delta", None),
                raw=resp,
                message=ChatMessage(
                    content=resp.get("choices", [{}])[0]
                    .get("message", {})
                    .get("content", "")
                ),
            )
        except HTTPException as http_exc:
            logger.error(f"HTTP exception during achat(): {http_exc.detail}")
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in achat(): {e}")
            raise HTTPException(
                status_code=500, detail=f"An unexpected error occurred: {str(e)}"
            )
        finally:
            # Clear params after the completion is done
            self.params = {}

    async def chat_completions_passthrough(
        self, chatCompletionsRequest: CompletionCreateParams, **kwargs: Any
    ) -> ChatCompletionResponse:
        try:
            if "/chat/completions" not in LLM_INFERENCE_URL:
                # If the URL does not support chat completions, raise an error
                raise HTTPException(
                    status_code=400,
                    detail=f"Chat completions not supported through endpoint {LLM_INFERENCE_URL}.",
                )

            client = await self._get_httpx_client()
            response = await client.post(
                LLM_INFERENCE_URL, json=chatCompletionsRequest, headers=DEFAULT_HEADERS
            )
            response.raise_for_status()  # Raise an exception for HTTP errors
            response_data = response.json()
            # Convert to ChatCompletionResponse with source_nodes=None for passthrough
            return ChatCompletionResponse(**response_data, source_nodes=None)
        except HTTPException as http_exc:
            logger.error(
                f"HTTP exception during chat completions passthrough: {http_exc.detail}"
            )
            raise http_exc
        except httpx.HTTPStatusError as e:
            logger.error(
                f"HTTP error {e.response.status_code} during POST request to {LLM_INFERENCE_URL}: {e.response.text}"
            )
            raise HTTPException(
                status_code=e.response.status_code, detail=f"{str(e.response.content)}"
            )
        except httpx.RequestError as e:
            logger.error(f"Error during POST request to {LLM_INFERENCE_URL}: {e}")
            raise HTTPException(
                status_code=500, detail=f"Error during POST request: {str(e)}"
            )
        except Exception as e:
            logger.error(f"Unexpected error during POST request: {e}")
            raise HTTPException(
                status_code=500, detail=f"Error during POST request: {str(e)}"
            )

    async def _async_completions(
        self, prompt: str, **kwargs: Any
    ) -> CompletionResponse:
        model_name, model_max_len = self._get_default_model_info()
        if kwargs.get("model"):
            model_name = kwargs.pop("model")
        data = {"prompt": prompt, **kwargs}
        if model_name:
            data["model"] = model_name  # Include the model only if it is not None
        if (
            model_max_len
            and data.get("max_tokens")
            and data["max_tokens"] > model_max_len
        ):
            logger.error(
                f"Requested max_tokens ({data['max_tokens']}) exceeds model's max length ({model_max_len})."
            )
            # vLLM will raise error ({"object":"error","message":"This model's maximum context length is 131072 tokens. However, you requested 500500500500505361 tokens (361 in the messages, 500500500500505000 in the completion). Please reduce the length of the messages or completion.","type":"BadRequestError","param":null,"code":400})

        # DEBUG: Call the debugging function
        # self._debug_curl_command(data)
        try:
            # add extended params
            for key, value in self.params.items():
                if key not in data:
                    data[key] = value
            resp = await self._async_post_request_raw(data, headers=DEFAULT_HEADERS)
            return self._completions_json_to_response(resp)
        except HTTPError as e:
            if not model_name and e.response.status_code == 400:
                logger.warning(
                    f"Potential issue with 'model' parameter in API response. "
                    f"Response: {str(e)}. Attempting to update the model name as a mitigation..."
                )
                self._default_model, self._default_max_model_len = (
                    self._fetch_default_model_info()
                )  # Fetch default model dynamically
                if self._default_model:
                    logger.info(
                        f"Default model '{self._default_model}' fetched successfully. Retrying request..."
                    )
                    data["model"] = self._default_model
                    resp = await self._async_post_request_raw(
                        data, headers=DEFAULT_HEADERS
                    )
                    return self._completions_json_to_response(resp)
                else:
                    logger.error("Failed to fetch a default model. Aborting retry.")
            raise  # Re-raise the exception if not recoverable
        except Exception as e:
            logger.error(f"An unexpected error occurred: {e}")
            raise

    def _completions_json_to_response(self, response_json: dict) -> CompletionResponse:
        """
        Converts the JSON response from the completions API to a CompletionResponse object.
        """
        # Check if the response contains OAI format
        if "choices" in response_json and response_json["choices"]:
            return CompletionResponse(text=response_json["choices"][0].get("text", ""))
        return CompletionResponse(text=str(response_json))

    def _get_models_endpoint(self) -> str:
        """
        Constructs the URL for the /v1/models endpoint based on LLM_INFERENCE_URL.
        """
        parsed = urlparse(LLM_INFERENCE_URL)
        return urljoin(f"{parsed.scheme}://{parsed.netloc}", "/v1/models")

    def _fetch_default_model_info(self) -> (str, int):
        """
        Fetch the default model name and max_length from the /v1/models endpoint.
        """
        try:
            models_url = self._get_models_endpoint()
            response = requests.get(models_url, headers=DEFAULT_HEADERS)
            response.raise_for_status()  # Raise an exception for HTTP errors (includes 404)

            models = response.json().get("data", [])
            if models:
                return models[0].get("id", None), models[0].get("max_model_len", None)
            return None, None

        except Exception as e:
            logger.error(
                f'Error fetching models from {models_url}: {e}. "model" parameter will not be included with inference call.'
            )
            return None

    def _get_default_model_info(self) -> (str, int):
        """
        Returns the cached default model if available, otherwise fetches and caches it.
        """
        if not self._default_model and not self._model_retrieval_attempted:
            self._model_retrieval_attempted = True
            self._default_model, self._default_max_model_len = (
                self._fetch_default_model_info()
            )
        return self._default_model, self._default_max_model_len

    async def _async_post_request_raw(self, data: dict, headers: dict):
        try:
            client = await self._get_httpx_client()
            response = await client.post(LLM_INFERENCE_URL, json=data, headers=headers)
            response.raise_for_status()  # Raise an exception for HTTP errors
            return response.json()
        except httpx.HTTPStatusError as e:
            logger.error(
                f"HTTP error {e.response.status_code} during POST request to {LLM_INFERENCE_URL}: {e.response.text}"
            )
            raise
        except httpx.RequestError as e:
            logger.error(f"Error during POST request to {LLM_INFERENCE_URL}: {e}")
            raise
        except Exception as e:
            logger.error(f"Unexpected error during POST request: {e}")
            raise

    def _debug_curl_command(self, data: dict) -> None:
        """
        Constructs and prints the equivalent curl command for debugging purposes.
        """
        import json

        # Construct curl command
        curl_command = (
            f"curl -X POST {LLM_INFERENCE_URL} "
            + " ".join(
                [
                    f'-H "{key}: {value}"'
                    for key, value in {
                        "Authorization": f"Bearer {LLM_ACCESS_SECRET}",
                        "Content-Type": "application/json",
                    }.items()
                ]
            )
            + f" -d '{json.dumps(data)}'"
        )
        logger.info("Equivalent curl command:")
        logger.info(curl_command)

    @property
    def metadata(self) -> LLMMetadata:
        """Get LLM metadata."""
        parsed_url = urlparse(LLM_INFERENCE_URL)
        path = parsed_url.path.lower()
        return LLMMetadata(
            is_chat_model="/chat/completions" in path,
            context_window=LLM_CONTEXT_WINDOW,
        )

    async def aclose(self):
        """Closes the HTTP client when shutting down."""
        if self._async_http_client:
            await self._async_http_client.aclose()
