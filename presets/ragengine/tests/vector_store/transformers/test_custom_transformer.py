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

from unittest import mock

from llama_index.core.schema import TextNode

from ragengine.vector_store.transformers.custom_transformer import CustomTransformer

# CustomTransformer.__call__ tests


def test_split_node_called_once_per_input_node():
    """Should call split_node exactly once per input node."""
    transformer = CustomTransformer()
    input_nodes = [
        TextNode(text="document one"),
        TextNode(text="document two"),
        TextNode(text="document three"),
    ]

    with mock.patch.object(
        CustomTransformer,
        "split_node",
        return_value=[TextNode(text="split")],
    ) as mock_split:
        transformer(input_nodes)
        expected = len(input_nodes)
        assert mock_split.call_count == expected, (
            f"split_node should be called {expected} times, "
            f"but was called {mock_split.call_count} times"
        )


def test_call_returns_all_split_results():
    """Should return the concatenation of all split_node results."""
    transformer = CustomTransformer()
    input_nodes = [TextNode(text="doc1"), TextNode(text="doc2")]
    split_a = TextNode(text="split_a")
    split_b = TextNode(text="split_b")
    split_c = TextNode(text="split_c")

    with mock.patch.object(
        CustomTransformer,
        "split_node",
        side_effect=[[split_a, split_b], [split_c]],
    ):
        result = transformer(input_nodes)
        expected = [split_a, split_b, split_c]
        assert result == expected


def test_call_with_empty_input():
    """Should return empty list without calling split_node for empty input."""
    transformer = CustomTransformer()

    with mock.patch.object(CustomTransformer, "split_node") as mock_split:
        result = transformer([])
        expected = []
        assert result == expected
        mock_split.assert_not_called()
