# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from llama_index.core.bridge.pydantic import PrivateAttr
from llama_index.core.node_parser import SentenceSplitter, CodeSplitter
from typing import Dict, List
from llama_index.core.schema import (
    TransformComponent,
    BaseNode,
)
from llama_index.core.utils import get_tqdm_iterable

class CustomTransformer(TransformComponent):
    """Custom transformer for splitting documents based on metadata input."""

    _code_splitters: Dict[str, CodeSplitter] = PrivateAttr()
    _sentence_splitter: SentenceSplitter = PrivateAttr()

    def __init__(self):
        super().__init__()
        self._code_splitters = {}
        self._sentence_splitter = SentenceSplitter()

    def split_node(self, node: BaseNode) -> List[BaseNode]:
        node_metadata = node.metadata
        split_type = node_metadata.get("split_type", "default")
        if split_type == "code":
            langauge = node_metadata.get("language", "")
            if not langauge:
                raise ValueError("Language not specified in node metadata.")
            
            if langauge not in self._code_splitters:
                self._code_splitters[langauge] = CodeSplitter(
                    language=langauge,
                )
            return self._code_splitters[langauge]([node])
        else:
            # Default to sentence splitting
            return self._sentence_splitter([node])
    
    def __call__(self, nodes, **kwargs):
        all_nodes: List[BaseNode] = []
        for node in nodes:
            nodes = self.split_node(node)
            all_nodes.extend(
                self.split_node(node)
            )
        return all_nodes
