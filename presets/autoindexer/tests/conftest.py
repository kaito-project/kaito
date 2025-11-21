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


"""
Shared test configuration and fixtures.
"""

import os

import pytest

# Set up environment variables at module level so they're available during imports
os.environ["NAMESPACE"] = "test-namespace"
os.environ["AUTOINDEXER_NAME"] = "test-autoindexer"
os.environ["ACCESS_SECRET"] = "test-secret"


@pytest.fixture(autouse=True, scope="session")
def setup_test_environment():
    """Ensure test environment variables remain set throughout the session."""
    # Environment variables are already set at module level
    # This fixture just ensures they stay set and cleans up at the end
    yield
    # Clean up after all tests are done
    for var in ["NAMESPACE", "AUTOINDEXER_NAME", "ACCESS_SECRET"]:
        os.environ.pop(var, None)


@pytest.fixture(autouse=True)
def reset_environment():
    """Reset environment variables before each test but preserve test globals."""
    original_env = os.environ.copy()
    yield
    # Restore environment but keep our test globals
    os.environ.clear()
    os.environ.update(original_env)
    # Ensure test environment variables are still set
    if "NAMESPACE" not in os.environ:
        os.environ["NAMESPACE"] = "test-namespace"
    if "AUTOINDEXER_NAME" not in os.environ:
        os.environ["AUTOINDEXER_NAME"] = "test-autoindexer"
    if "ACCESS_SECRET" not in os.environ:
        os.environ["ACCESS_SECRET"] = "test-access-secret"