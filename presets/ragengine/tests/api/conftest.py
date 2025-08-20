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
import sys

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../..")))
os.environ["CUDA_VISIBLE_DEVICES"] = "-1"  # Force CPU-only execution for testing
# Force single-threaded for testing to prevent segfault while loading embedding model
os.environ["OMP_NUM_THREADS"] = "1"
os.environ["MKL_NUM_THREADS"] = "1"  # Force MKL to use a single thread

import asyncio

import httpx
import nest_asyncio
import pytest_asyncio

from ragengine.main import app, vector_store_handler

nest_asyncio.apply()


@pytest_asyncio.fixture
async def async_client():
    """Use an async HTTP client to interact with FastAPI app."""
    async with httpx.AsyncClient(app=app, base_url="http://localhost") as client:
        yield client


@pytest_asyncio.fixture(scope="session")
def event_loop():
    loop = asyncio.new_event_loop()
    yield loop
    loop.close()


@pytest_asyncio.fixture(autouse=True)
def clear_index():
    vector_store_handler.index_map.clear()
