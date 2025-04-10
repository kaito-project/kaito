# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import time
from functools import wraps
from .prometheus_metrics import (
    rag_embedding_latency,
    rag_embedding_success,
    rag_embedding_failure
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
                rag_embedding_failure.inc()
            else:
                # Record successful embedding
                rag_embedding_success.inc()
            
        except Exception as e:
            # Record failed embedding
            rag_embedding_failure.inc()
            # Record latency even for failures
            rag_embedding_latency.observe(time.time() - start_time)
            raise e
        # Record latency
        rag_embedding_latency.observe(time.time() - start_time)
        return result
    return wrapper
