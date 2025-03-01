# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from pydantic import PrivateAttr
import logging
import asyncio
import httpx
from typing import Any
from llama_index.core.llms import CustomLLM, CompletionResponse, LLMMetadata, CompletionResponseGen
from llama_index.llms.openai import OpenAI
from llama_index.core.llms.callbacks import llm_completion_callback
import requests
from requests.exceptions import HTTPError
from urllib.parse import urlparse, urljoin
from ragengine.config import LLM_INFERENCE_URL, LLM_ACCESS_SECRET #, LLM_RESPONSE_FIELD
from fastapi import HTTPException
import concurrent.futures

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

OPENAI_URL_PREFIX = "https://api.openai.com"
HUGGINGFACE_URL_PREFIX = "https://api-inference.huggingface.co"
DEFAULT_HEADERS = {
    "Authorization": f"Bearer {LLM_ACCESS_SECRET}",
    "Content-Type": "application/json"
}
DEFAULT_HTTP_TIMEOUT = 300.0 # Seconds
DEFAULT_HTTP_SUCCESS_CODE = 200

class Inference(CustomLLM):
    params: dict = {}
    _default_model: str = None
    _default_max_model_len: int = None
    _model_retrieval_attempted: bool = False
    _async_http_client : httpx.AsyncClient = PrivateAttr(default=None)

    async def _get_httpx_client(self):
        """ Lazily initializes the HTTP client on first request. """
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
    def complete(self, prompt: str, formatted: bool = False, **kwargs: Any) -> CompletionResponse:
        # Required implementation - Only called by LlamaIndex reranker because LLMRerank library doesn't use async call
        try:
            result = self.run_async_coroutine(self.acomplete(prompt, formatted=formatted, **kwargs))
            if result.text == "Empty Response":
                logger.error("LLMRerank Request returned an unparsable or invalid response")
                raise HTTPException(status_code=422, detail="Rerank operation failed: Invalid response from LLM. This feature is experimental.")
            return result
        except HTTPException as http_exc:
            # If it's already an HTTPException (e.g., 422), re-raise it as is
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in complete(): {e}")
            raise HTTPException(status_code=500, detail=f"An unexpected error occurred: {str(e)}")

    @llm_completion_callback()
    async def acomplete(self, prompt: str, formatted: bool = False, **kwargs: Any) -> CompletionResponse:
        try:
            if LLM_INFERENCE_URL.startswith(OPENAI_URL_PREFIX):
                return await self._async_openai_complete(prompt, **kwargs, **self.params)
            elif LLM_INFERENCE_URL.startswith(HUGGINGFACE_URL_PREFIX):
                return await self._async_huggingface_remote_complete(prompt, **kwargs, **self.params)
            else:
                return await self._async_custom_api_complete(prompt, **kwargs, **self.params)
        except HTTPException as http_exc:
            raise http_exc
        except Exception as e:
            logger.error(f"Unexpected exception in acomplete(): {e}")
            raise HTTPException(status_code=500, detail=f"An unexpected error occurred: {str(e)}")
        finally:
            # Clear params after the completion is done
            self.params = {}

    async def _async_openai_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        return await OpenAI(api_key=LLM_ACCESS_SECRET, **kwargs).acomplete(prompt)

    async def _async_huggingface_remote_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        data = {"prompt": prompt, **kwargs}
        return await self._async_post_request(data, headers={"Authorization": f"Bearer {LLM_ACCESS_SECRET}", "Content-Type": "application/json"})
    async def _async_custom_api_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        model_name, model_max_len = self._get_default_model_info()
        if kwargs.get("model"):
            model_name = kwargs.pop("model")
        data = {"prompt": prompt, **kwargs}
        if model_name:
            data["model"] = model_name # Include the model only if it is not None
        if model_max_len and data.get("max_tokens"):
            if data["max_tokens"] > model_max_len:
                logger.error(f"Requested max_tokens ({data['max_tokens']}) exceeds model's max length ({model_max_len}).")
                # vLLM will raise error ({"object":"error","message":"This model's maximum context length is 131072 tokens. However, you requested 500500500500505361 tokens (361 in the messages, 500500500500505000 in the completion). Please reduce the length of the messages or completion.","type":"BadRequestError","param":null,"code":400})

        # DEBUG: Call the debugging function
        # self._debug_curl_command(data)
        try:
            return await self._async_post_request(data, headers=DEFAULT_HEADERS)
        except HTTPError as e:
            if not model_name and e.response.status_code == 400:
                logger.warning(
                    f"Potential issue with 'model' parameter in API response. "
                    f"Response: {str(e)}. Attempting to update the model name as a mitigation..."
                )
                self._default_model = self._fetch_default_model()  # Fetch default model dynamically
                if self._default_model:
                    logger.info(f"Default model '{self._default_model}' fetched successfully. Retrying request...")
                    data["model"] = self._default_model
                    return await self._async_post_request(data, headers=DEFAULT_HEADERS)
                else:
                    logger.error("Failed to fetch a default model. Aborting retry.")
            raise  # Re-raise the exception if not recoverable
        except Exception as e:
            logger.error(f"An unexpected error occurred: {e}")
            raise

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
            logger.error(f"Error fetching models from {models_url}: {e}. \"model\" parameter will not be included with inference call.")
            return None

    def _get_default_model_info(self) -> (str, int):
        """
        Returns the cached default model if available, otherwise fetches and caches it.
        """
        if not self._default_model and not self._model_retrieval_attempted:
            self._model_retrieval_attempted = True
            self._default_model, self._default_max_model_len = self._fetch_default_model_info()
        return self._default_model, self._default_max_model_len

    async def _async_post_request(self, data: dict, headers: dict) -> CompletionResponse:
        try:
            client = await self._get_httpx_client()
            response = await client.post(LLM_INFERENCE_URL, json=data, headers=headers)
            response.raise_for_status()  # Raise an exception for HTTP errors
            response_data = response.json()
            # Check if the request was successful
            if response.status_code == DEFAULT_HTTP_SUCCESS_CODE:
                # OAI Spec returns array of choices, we choose the first one
                if "choices" in response_data and response_data["choices"]:
                    return CompletionResponse(text=response_data["choices"][0].get("text", ""))
            # Return full response as text if no specific parsing is possible
            return CompletionResponse(text=str(response_data))
        except httpx.HTTPStatusError as e:
            logger.error(f"HTTP error {e.response.status_code} during POST request to {LLM_INFERENCE_URL}: {e.response.text}")
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
                + " ".join([f'-H "{key}: {value}"' for key, value in {
            "Authorization": f"Bearer {LLM_ACCESS_SECRET}",
            "Content-Type": "application/json"
        }.items()])
                + f" -d '{json.dumps(data)}'"
        )
        logger.info("Equivalent curl command:")
        logger.info(curl_command)

    @property
    def metadata(self) -> LLMMetadata:
        """Get LLM metadata."""
        return LLMMetadata()

    async def aclose(self):
        """ Closes the HTTP client when shutting down. """
        if self._async_http_client:
            await self._async_http_client.aclose()
