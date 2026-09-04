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

"""CPU and memory resource collection for guardrails benchmarking."""

import resource
import time
import tracemalloc
from contextlib import contextmanager
from dataclasses import dataclass


@dataclass
class ResourceSnapshot:
    """Resource usage for a single operation."""

    cpu_time_ms: float = 0.0
    memory_peak_kb: float = 0.0


@contextmanager
def measure_resources():
    """Context manager that captures CPU time and peak memory allocation."""
    snapshot = ResourceSnapshot()

    # CPU time (user + system)
    cpu_start = time.process_time_ns()

    # Memory tracking
    was_tracing = tracemalloc.is_tracing()
    if not was_tracing:
        tracemalloc.start()
    tracemalloc.reset_peak()

    try:
        yield snapshot
    finally:
        cpu_end = time.process_time_ns()
        snapshot.cpu_time_ms = (cpu_end - cpu_start) / 1_000_000

        _, peak = tracemalloc.get_traced_memory()
        snapshot.memory_peak_kb = peak / 1024

        if not was_tracing:
            tracemalloc.stop()


@contextmanager
def measure_cpu_only():
    """Lightweight context manager for CPU time only."""
    snapshot = ResourceSnapshot()
    cpu_start = time.process_time_ns()
    try:
        yield snapshot
    finally:
        cpu_end = time.process_time_ns()
        snapshot.cpu_time_ms = (cpu_end - cpu_start) / 1_000_000


def get_rss_kb() -> float:
    """Get current RSS in KB (Linux/macOS)."""
    usage = resource.getrusage(resource.RUSAGE_SELF)
    # ru_maxrss is in KB on Linux, bytes on macOS
    return float(usage.ru_maxrss)
