from typing import Any, Optional
from llama_index.core.llms import CustomLLM, CompletionResponse, LLMMetadata, CompletionResponseGen
from llama_index.llms.openai import OpenAI
from llama_index.core.llms.callbacks import llm_completion_callback
import requests
from config import INFERENCE_URL, INFERENCE_ACCESS_SECRET, RESPONSE_FIELD

class CustomInference(CustomLLM):

    @llm_completion_callback()
    def stream_complete(self, prompt: str, **kwargs: Any) -> CompletionResponseGen:
        pass

    @llm_completion_callback()
    def complete(self, prompt: str, **kwargs) -> CompletionResponse:
        if "openai" in INFERENCE_URL:
            return self._openai_complete(prompt, **kwargs)
        else:
            return self._custom_api_complete(prompt, **kwargs)

    def _openai_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        llm = OpenAI(
            api_key=INFERENCE_ACCESS_SECRET,
            **kwargs  # Pass all kwargs directly; kwargs may include model, temperature, max_tokens, etc.
        )
        return llm.complete(prompt)

    def _custom_api_complete(self, prompt: str, **kwargs: Any) -> CompletionResponse:
        headers = {"Authorization": f"Bearer {INFERENCE_ACCESS_SECRET}"}
        data = {"prompt": prompt, **kwargs}

        response = requests.post(INFERENCE_URL, json=data, headers=headers)
        response_data = response.json()

        # Dynamically extract the field from the response based on the specified response_field
        completion_text = response_data.get(RESPONSE_FIELD, "No response field found")

        return CompletionResponse(text=completion_text)

    @property
    def metadata(self) -> LLMMetadata:
        """Get LLM metadata."""
        return LLMMetadata()