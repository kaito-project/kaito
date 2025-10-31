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


import os
from tempfile import TemporaryDirectory

import pytest

from ragengine.models import Document
from ragengine.tests.vector_store.test_base_store import BaseVectorStoreTest
from ragengine.vector_store.faiss_store import FaissVectorStoreHandler


class TestFaissVectorStore(BaseVectorStoreTest):
    """Test implementation for FAISS vector store."""

    @pytest.fixture
    def vector_store_manager(self, init_embed_manager):
        with TemporaryDirectory() as temp_dir:
            print(f"Saving temporary test storage at: {temp_dir}")
            os.environ["PERSIST_DIR"] = temp_dir
            yield FaissVectorStoreHandler(init_embed_manager)

    @pytest.mark.asyncio
    async def check_indexed_documents(self, vector_store_manager):
        expected_output_1 = [
            Document(
                doc_id="",
                text="First document in index1",
                metadata={"type": "text"},
                hash_value="1e64a170be48c45efeaa8667ab35919106da0489ec99a11d0029f2842db133aa",
                is_truncated=False,
            )
        ]
        expected_output_2 = [
            Document(
                doc_id="",
                text="First document in index2",
                metadata={"type": "text"},
                hash_value="a222f875b83ce8b6eb72b3cae278b620de9bcc7c6b73222424d3ce979d1a463b",
                is_truncated=False,
            )
        ]

        for index, expected_output in zip(
            ["index1", "index2"], [expected_output_1, expected_output_2], strict=False
        ):
            resp = await vector_store_manager.list_documents_in_index(
                index, limit=10, offset=0, max_text_length=1000
            )

            assert all(
                resp_doc.text == expected_doc.text
                and resp_doc.hash_value == expected_doc.hash_value
                and resp_doc.metadata == expected_doc.metadata
                for resp_doc, expected_doc in zip(resp.documents, expected_output)
            )

    @property
    def expected_query_score(self):
        """Override this in implementation-specific test classes."""
        return 0.5795239210128784
