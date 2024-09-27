from llama_index.embeddings.huggingface import HuggingFaceEmbedding

from .base import BaseEmbeddingModel


class LocalHuggingFaceEmbedding(BaseEmbeddingModel):
    def __init__(self, model_name: str):
        self.model = HuggingFaceEmbedding(model_name=model_name)

    def get_text_embedding(self, text: str):
        """Returns the text embedding for a given input string."""
        return self.model.get_text_embedding(text)

    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a local call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self.get_text_embedding(dummy_input)
        
        # TODO Assume embedding is a 1D array (needs to be tested); return its length (the dimension size)
        return len(embedding)