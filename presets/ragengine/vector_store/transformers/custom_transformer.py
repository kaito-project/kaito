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


from llama_index.core.bridge.pydantic import PrivateAttr
from llama_index.core.node_parser import CodeSplitter, SentenceSplitter
from llama_index.core.schema import (
    BaseNode,
    TransformComponent,
)


class CustomTransformer(TransformComponent):
    """Custom transformer for splitting documents based on metadata input."""

    _code_splitters: dict[str, CodeSplitter] = PrivateAttr()
    _sentence_splitter: SentenceSplitter = PrivateAttr()

    def __init__(self):
        super().__init__()
        self._code_splitters = {}
        self._sentence_splitter = SentenceSplitter()

    def split_node(self, node: BaseNode) -> list[BaseNode]:
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
        all_nodes: list[BaseNode] = []
        for node in nodes:
            nodes = self.split_node(node)
            all_nodes.extend(self.split_node(node))
        return all_nodes
