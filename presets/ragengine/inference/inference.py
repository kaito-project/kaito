# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Any
from llama_index.core.llms import CustomLLM, CompletionResponse, LLMMetadata, CompletionResponseGen
from llama_index.llms.openai import OpenAI
from llama_index.core.llms.callbacks import llm_completion_callback
import requests
from urllib.parse import urlparse, urljoin
from ragengine.config import LLM_INFERENCE_URL, LLM_ACCESS_SECRET #, LLM_RESPONSE_FIELD

OPENAI_URL_PREFIX = "https://api.openai.com"
HUGGINGFACE_URL_PREFIX = "https://api-inference.huggingface.co"

class Inference(CustomLLM):
    params: dict = {}
    model: str = ""

    def set_params(self, params: dict) -> None:
        self.params = params

    def get_param(self, key, default=None):
        return self.params.get(key, default)
    # Get base URL 
    def _get_base_url(self) -> str:
        parsed = urlparse(LLM_INFERENCE_URL)
        base_url = f"{parsed.scheme}://{parsed.netloc}"
        return urljoin(base_url, "/v1/models")
    
    #Fetch and set the model from the inference endpoint
    def set_model(self) -> None:
        
        try:
            models_url = self._get_base_url()
            headers = {"Authorization": f"Bearer {LLM_ACCESS_SECRET}"}
            response = requests.get(models_url, headers=headers)

            if response.status_code == 404:
                self.model = None
                return
            
            response.raise_for_status()
            
            data = response.json()
            if data.get("data") and len(data["data"]) > 0:
                self.model = data["data"][0]["id"]
            else:
                raise ValueError("No model found in response")
                
        except requests.RequestException as e:
            raise Exception(f"Failed to fetch model information: {str(e)}")

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
        if self.model != None:
            data = {"prompt": prompt, "model":self.model}
        else:
            data = {"prompt": prompt}
 
        for param in self.params:
            data[param] = self.params[param]
                
        response = requests.post(LLM_INFERENCE_URL, json=data, headers=headers)
        response_data = response.json()

        # Dynamically extract the field from the response based on the specified response_field
        # completion_text = response_data.get(RESPONSE_FIELD, "No response field found") # not necessary for now
        return CompletionResponse(text=str(response_data))

    @property
    def metadata(self) -> LLMMetadata:
        """Get LLM metadata."""
        return LLMMetadata()
