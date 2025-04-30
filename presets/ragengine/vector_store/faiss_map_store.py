import numpy as np
from typing import Any, List, cast

from llama_index.core.schema import BaseNode
from llama_index.core.bridge.pydantic import PrivateAttr
from llama_index.vector_stores.faiss import FaissVectorStore

class FaissVectorMapStore(FaissVectorStore):
    ref_doc_id_map: dict = {}

    def __init__(
        self,
        faiss_index: Any,
    ) -> None:
        """Initialize params."""
        import_err_msg = """
            `faiss` package not found. For instructions on
            how to install `faiss` please visit
            https://github.com/facebookresearch/faiss/wiki/Installing-Faiss
        """
        try:
            import faiss
        except ImportError:
            raise ImportError(import_err_msg)
        super().__init__(faiss_index=faiss_index)

    def add(
        self,
        nodes: List[BaseNode],
        **add_kwargs: Any,
    ) -> List[str]:
        """Add nodes to index.

        NOTE: in the Faiss vector store, we do not store text in Faiss.

        Args:
            nodes: List[BaseNode]: list of nodes with embeddings

        """
        new_ids = []
        for node in nodes:
            text_embedding = node.get_embedding()
            text_embedding_np = np.array(text_embedding, dtype="float32")[np.newaxis, :]
            new_id = str(self._faiss_index.ntotal)
            self.ref_doc_id_map[node.ref_doc_id] = self._faiss_index.ntotal
            self._faiss_index.add_with_ids(text_embedding_np, self._faiss_index.ntotal)
            new_ids.append(new_id)
        return new_ids

    def delete(self, ref_doc_id: str, **delete_kwargs: Any) -> None:
        """
        Delete nodes using with ref_doc_id.

        Args:
            ref_doc_id (str): The doc_id of the document to delete.

        """
        if ref_doc_id in self.ref_doc_id_map:
            self._faiss_index.remove_ids(np.array([int(self.ref_doc_id_map[ref_doc_id])], dtype=np.int64))
            del self.ref_doc_id_map[ref_doc_id]