# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import logging
from typing import Any
from dataclasses import field
from llama_index.core.llms import CustomLLM, CompletionResponse, LLMMetadata, CompletionResponseGen
from llama_index.llms.openai import OpenAI
from llama_index.core.llms.callbacks import llm_completion_callback
import requests
from urllib.parse import urlparse, urljoin
from ragengine.config import LLM_INFERENCE_URL, LLM_ACCESS_SECRET #, LLM_RESPONSE_FIELD

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

OPENAI_URL_PREFIX = "https://api.openai.com"
HUGGINGFACE_URL_PREFIX = "https://api-inference.huggingface.co"
DEFAULT_HEADERS = {
    "Authorization": f"Bearer {LLM_ACCESS_SECRET}",
    "Content-Type": "application/json"
}

class Inference(CustomLLM):
    params: dict = {}
    _default_model: str = None
    _model_retrieval_attempted: bool = False

    def set_params(self, params: dict) -> None:
        self.params = params

    def get_param(self, key, default=None):
        return self.params.get(key, default)

    @llm_completion_callback()
    def stream_complete(self, prompt: str, **kwargs: Any) -> CompletionResponseGen:
        pass

    @llm_completion_callback()
    def complete(self, prompt: str, formatted: bool, **kwargs) -> CompletionResponse:
        try:
            if LLM_INFERENCE_URL.startswith(OPENAI_URL_PREFIX):
                return self._openai_complete(prompt, **kwargs, **self.params)
            elif LLM_INFERENCE_URL.startswith(HUGGINGFACE_URL_PREFIX):
                return self._huggingface_remote_complete(prompt, **kwargs, **self.params)
            else:
                return self._custom_api_complete(prompt, **kwargs, **self.params)
        finally:
            # Clear params after the completion is done
            self.params = {}

    def _openai_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        return OpenAI(api_key=LLM_ACCESS_SECRET, **kwargs).complete(prompt)

    def _huggingface_remote_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        return self._post_request(
            {"messages": [{"role": "user", "content": prompt}]},
            headers={"Authorization": f"Bearer {LLM_ACCESS_SECRET}"}
        )

    def _custom_api_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        model = kwargs.pop("model", self._get_default_model())
        data = {"prompt": prompt, **kwargs}
        if model:
            data["model"] = model # Include the model only if it is not None

        # DEBUG: Call the debugging function
        # self._debug_curl_command(data)
        return self._post_request(data, headers=DEFAULT_HEADERS)

    def _get_models_endpoint(self) -> str:
        """
        Constructs the URL for the /v1/models endpoint based on LLM_INFERENCE_URL.
        """
        parsed = urlparse(LLM_INFERENCE_URL)
        return urljoin(f"{parsed.scheme}://{parsed.netloc}", "/v1/models")

    def _fetch_default_model(self) -> str:
        """
        Fetch the default model from the /v1/models endpoint.
        """
        try:
            models_url = self._get_models_endpoint()
            response = requests.get(models_url, headers=DEFAULT_HEADERS)
            response.raise_for_status()  # Raise an exception for HTTP errors (includes 404)

            models = response.json().get("data", [])
            return models[0].get("id") if models else None
        except Exception as e:
            logger.error(f"Error fetching models from {models_url}: {e}. \"model\" parameter will not be included with inference call.")
            return None

    def _get_default_model(self) -> str:
        """
        Returns the cached default model if available, otherwise fetches and caches it.
        """
        if not self._default_model and not self._model_retrieval_attempted:
            self._model_retrieval_attempted = True
            self._default_model = self._fetch_default_model()
        return self._default_model

    def _post_request(self, data: dict, headers: dict) -> CompletionResponse:
        try:
            response = requests.post(LLM_INFERENCE_URL, json=data, headers=headers)
            response.raise_for_status()  # Raise exception for HTTP errors
            response_data = response.json()
            return CompletionResponse(text=str(response_data))
        except requests.RequestException as e:
            logger.error(f"Error during POST request to {LLM_INFERENCE_URL}: {e}")
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
