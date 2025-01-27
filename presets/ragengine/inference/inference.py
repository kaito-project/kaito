# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Any
from llama_index.core.llms import CustomLLM, CompletionResponse, LLMMetadata, CompletionResponseGen
from llama_index.llms.openai import OpenAI
from llama_index.core.llms.callbacks import llm_completion_callback
import requests
from ragengine.config import LLM_INFERENCE_URL, LLM_ACCESS_SECRET #, LLM_RESPONSE_FIELD

OPENAI_URL_PREFIX = "https://api.openai.com"
HUGGINGFACE_URL_PREFIX = "https://api-inference.huggingface.co"

class Inference(CustomLLM):
    params: dict = {}

    def set_params(self, params: dict) -> None:
        self.params = params

    def get_param(self, key, default=None):
        return self.params.get(key, default)

    @llm_completion_callback()
    def stream_complete(self, prompt: str, **kwargs: Any) -> CompletionResponseGen:
        pass

    @llm_completion_callback()
    def complete(self, prompt: str, **kwargs) -> CompletionResponse:
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
        llm = OpenAI(
            api_key=LLM_ACCESS_SECRET,
            **kwargs  # Pass all kwargs directly; kwargs may include model, temperature, max_tokens, etc.
        )
        return llm.complete(prompt)

    def _huggingface_remote_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        headers = {"Authorization": f"Bearer {LLM_ACCESS_SECRET}"}
        data = {"messages": [{"role": "user", "content": prompt}]}
        response = requests.post(LLM_INFERENCE_URL, json=data, headers=headers)
        response_data = response.json()
        return CompletionResponse(text=str(response_data))

    def _custom_api_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        headers = {"Authorization": f"Bearer {LLM_ACCESS_SECRET}"}
        data = {"prompt": prompt, **kwargs}

        response = requests.post(LLM_INFERENCE_URL, json=data, headers=headers)
        response_data = response.json()

        # Dynamically extract the field from the response based on the specified response_field
        # completion_text = response_data.get(RESPONSE_FIELD, "No response field found") # not necessary for now
        return CompletionResponse(text=str(response_data))

    @property
    def metadata(self) -> LLMMetadata:
        """Get LLM metadata."""
        return LLMMetadata()
