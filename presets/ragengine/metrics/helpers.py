# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import time
from functools import wraps
import inspect
from .prometheus_metrics import (
    rag_embedding_requests_total,
    rag_embedding_latency,
    STATUS_SUCCESS,
    STATUS_FAILURE,
    MODE_REMOTE 
)

def record_embedding_metrics(func):
    """
    Decorator to record embedding metrics for synchronous functions.
    Must be used within a context where the metrics are already imported.
    """
    @wraps(func)
    def wrapper(*args, **kwargs):
        start_time = time.time()
        try:
            result = func(*args, **kwargs)
            if result is None:
                # Record failed embedding
                rag_embedding_requests_total.labels(status=STATUS_FAILURE, mode=MODE_REMOTE).inc()
                # Record latency even for failures
                rag_embedding_latency.labels(status=STATUS_FAILURE, mode=MODE_REMOTE).observe(time.time() - start_time)
            else:
                # Record successful embedding
                rag_embedding_requests_total.labels(status=STATUS_SUCCESS, mode=MODE_REMOTE).inc()
                # Record latency
                rag_embedding_latency.labels(status=STATUS_SUCCESS, mode=MODE_REMOTE).observe(time.time() - start_time)
            return result
        except Exception as e:
            # Record failed embedding
            rag_embedding_requests_total.labels(status=STATUS_SUCCESS, mode=MODE_REMOTE).inc()
            # Record latency even for failures
            rag_embedding_latency.labels(status=STATUS_SUCCESS, mode=MODE_REMOTE).observe(time.time() - start_time)
            raise e

    return wrapper
