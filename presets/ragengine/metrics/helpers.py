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


import time
from functools import wraps

from .prometheus_metrics import (
    MODE_REMOTE,
    STATUS_FAILURE,
    STATUS_SUCCESS,
    rag_embedding_latency,
    rag_embedding_requests_total,
)


def record_embedding_metrics(func):
    """
    Decorator to record embedding metrics for synchronous functions.
    Must be used within a context where the metrics are already imported.
    """

    @wraps(func)
    def wrapper(*args, **kwargs):
        start_time = time.perf_counter()
        status = STATUS_FAILURE  # Default to failure

        try:
            result = func(*args, **kwargs)
            status = STATUS_FAILURE if result is None else STATUS_SUCCESS
            return result
        except Exception:
            # Status remains STATUS_FAILURE
            raise
        finally:
            # Record metrics once in finally block
            latency = time.perf_counter() - start_time
            rag_embedding_requests_total.labels(status=status, mode=MODE_REMOTE).inc()
            rag_embedding_latency.labels(status=status, mode=MODE_REMOTE).observe(
                latency
            )

    return wrapper
