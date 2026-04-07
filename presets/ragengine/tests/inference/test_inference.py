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

import pytest

from ragengine.inference.inference import Inference


class MockResponse:
    def __init__(self, json_payload, status_code=200):
        self.json_payload = json_payload
        self.status_code = status_code

    def json(self):
        return self.json_payload

    def raise_for_status(self):
        if self.status_code == 200:
            return True
        raise Exception("Mock error")


# _fetch_default_model_info tests

def test_fetch_default_model_info_returns_tuple_on_error():
    """Should return (None, None) tuple when models endpoint is unreachable."""
    inference = Inference()
    with mock.patch(
        "ragengine.inference.inference.requests.get",
        side_effect=Exception("connection refused"),
    ):
        result = inference._fetch_default_model_info()
        expected = (None, None)
        assert isinstance(result, tuple), f"Expected tuple, got {type(result)}"
        assert result == expected


def test_fetch_default_model_info_returns_tuple_on_empty_models():
    """Should return (None, None) when models endpoint returns empty data."""
    response = MockResponse(json_payload={"data": []})
    inference = Inference()
    with mock.patch(
        "ragengine.inference.inference.requests.get",
        return_value=response,
    ):
        result = inference._fetch_default_model_info()
        expected = (None, None)
        assert isinstance(result, tuple)
        assert result == expected


def test_fetch_default_model_info_returns_model_info_on_success():
    """Should return (model_id, max_len) when models endpoint returns data."""
    response = MockResponse(
        json_payload={"data": [{"id": "test-model", "max_model_len": 4096}]}
    )
    inference = Inference()
    with mock.patch(
        "ragengine.inference.inference.requests.get",
        return_value=response,
    ):
        result = inference._fetch_default_model_info()
        expected = ("test-model", 4096)
        assert isinstance(result, tuple)
        assert result == expected
