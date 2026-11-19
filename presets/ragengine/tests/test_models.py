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

import pytest
from llama_index.core.base.llms.types import MessageRole

from ragengine.models import (
    get_message_content,
    input_messages_to_llamaindex_messages,
)

TEXT_PARTS = [
    {"type": "text", "text": "Follow the style guide."},
    {"type": "text", "text": "Be brief."},
]
TEXT_PARTS_JOINED = "Follow the style guide.\nBe brief."


@pytest.mark.parametrize("role", ["system", "developer", "assistant", "user"])
@pytest.mark.parametrize(
    ("content", "expected"),
    [
        ("plain text", "plain text"),
        ({"type": "text", "text": "one part"}, "one part"),
        (TEXT_PARTS, TEXT_PARTS_JOINED),
        (["bare string", {"type": "text", "text": "part"}], "bare string\npart"),
        (
            [
                {"type": "text", "text": "kept"},
                {"type": "refusal", "refusal": "dropped"},
            ],
            "kept",
        ),
    ],
    ids=["string", "single-part", "parts", "string-and-part", "skips-non-text"],
)
def test_get_message_content_returns_text(role, content, expected):
    assert get_message_content({"role": role, "content": content}) == expected


@pytest.mark.parametrize("role", ["assistant", "user"])
@pytest.mark.parametrize("message_content", [None, ""])
def test_get_message_content_empty_content_is_empty_string(role, message_content):
    assert get_message_content({"role": role, "content": message_content}) == ""
    assert get_message_content({"role": role}) == ""


@pytest.mark.parametrize(
    ("role", "message_role"),
    [
        ("system", MessageRole.SYSTEM),
        ("developer", MessageRole.DEVELOPER),
        ("assistant", MessageRole.ASSISTANT),
        ("user", MessageRole.USER),
    ],
)
def test_input_messages_accept_text_part_arrays(role, message_role):
    messages = input_messages_to_llamaindex_messages(
        [{"role": role, "content": TEXT_PARTS}]
    )

    assert len(messages) == 1
    assert messages[0].role == message_role
    assert messages[0].content == TEXT_PARTS_JOINED


def test_input_messages_accept_assistant_message_with_null_content():
    messages = input_messages_to_llamaindex_messages(
        [{"role": "assistant", "content": None}]
    )

    assert messages[0].role == MessageRole.ASSISTANT
    assert messages[0].content == ""
