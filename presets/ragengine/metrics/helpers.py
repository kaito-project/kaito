# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import time
from functools import wraps
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
        start_time = time.perf_counter()  
        status = STATUS_FAILURE  # Default to failure
        
        try:
            result = func(*args, **kwargs)
            if result is None:
                # None result is considered a failure
                status = STATUS_FAILURE
            else:
                # Successful embedding
                status = STATUS_SUCCESS
            return result
        except Exception:
            # Status remains STATUS_FAILURE
            raise
        finally:
            # Record metrics once in finally block
            latency = time.perf_counter() - start_time
            rag_embedding_requests_total.labels(status=status, mode=MODE_REMOTE).inc()
            rag_embedding_latency.labels(status=status, mode=MODE_REMOTE).observe(latency)

    return wrapper
