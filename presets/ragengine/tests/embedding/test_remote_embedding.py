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

import json
from unittest.mock import MagicMock, patch

from ragengine.embedding.remote_embedding import RemoteEmbeddingModel

EMBEDDING_URL = "http://embeddings.example.com/embed"
TOKEN = "secret-token"


def _mock_response(embedding):
    response = MagicMock()
    response.json.return_value = embedding
    response.raise_for_status.return_value = None
    return response


@patch("requests.post")
def test_remote_embedding_posts_inputs_with_bearer_token(mock_post):
    mock_post.return_value = _mock_response([0.1, 0.2, 0.3])

    model = RemoteEmbeddingModel(EMBEDDING_URL, TOKEN)
    embedding = model.get_text_embedding("hello world")

    assert embedding == [0.1, 0.2, 0.3]
    mock_post.assert_called_once()
    args, kwargs = mock_post.call_args
    assert args[0] == EMBEDDING_URL
    assert kwargs["headers"]["Authorization"] == f"Bearer {TOKEN}"
    assert json.loads(kwargs["data"]) == {"inputs": "hello world"}


@patch("requests.post")
def test_remote_embedding_dimension(mock_post):
    mock_post.return_value = _mock_response([0.0] * 384)

    model = RemoteEmbeddingModel(EMBEDDING_URL, TOKEN)

    assert model.get_embedding_dimension() == 384


def test_remote_embedding_token_not_exposed():
    model = RemoteEmbeddingModel(EMBEDDING_URL, TOKEN)

    assert model.model_url == EMBEDDING_URL
    assert TOKEN not in repr(model)
    assert TOKEN not in json.dumps(model.to_dict())
