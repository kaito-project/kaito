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

"""Timing and statistics collection for guardrails benchmarking."""

import json
import statistics
import time
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class RequestTimestamps:
    """Timestamps captured per request."""

    request_start: float = 0.0
    upstream_first_content: float = 0.0
    client_first_content: float = 0.0
    response_complete: float = 0.0

    @property
    def e2e_latency_ms(self) -> float:
        return (self.response_complete - self.request_start) * 1000

    @property
    def ttft_delay_ms(self) -> float:
        """Guardrail-induced first-visible-token delay (streaming only)."""
        if self.client_first_content == 0 or self.upstream_first_content == 0:
            return 0.0
        return (self.client_first_content - self.upstream_first_content) * 1000

    @property
    def client_ttft_ms(self) -> float:
        """Client-visible TTFT."""
        if self.client_first_content == 0:
            return 0.0
        return (self.client_first_content - self.request_start) * 1000


@dataclass
class LatencyStats:
    """Percentile statistics for a list of latency values."""

    p50: float
    p95: float
    p99: float
    mean: float
    stddev: float
    count: int
    min_val: float
    max_val: float

    @classmethod
    def from_values(cls, values: list[float]) -> "LatencyStats":
        if not values:
            return cls(0, 0, 0, 0, 0, 0, 0, 0)
        sorted_vals = sorted(values)
        n = len(sorted_vals)
        return cls(
            p50=sorted_vals[int(n * 0.50)],
            p95=sorted_vals[int(n * 0.95)] if n >= 20 else sorted_vals[-1],
            p99=sorted_vals[int(n * 0.99)] if n >= 100 else sorted_vals[-1],
            mean=statistics.mean(sorted_vals),
            stddev=statistics.stdev(sorted_vals) if n > 1 else 0.0,
            count=n,
            min_val=sorted_vals[0],
            max_val=sorted_vals[-1],
        )

    def to_dict(self) -> dict:
        return {
            "p50": round(self.p50, 3),
            "p95": round(self.p95, 3),
            "p99": round(self.p99, 3),
            "mean": round(self.mean, 3),
            "stddev": round(self.stddev, 3),
            "count": self.count,
            "min": round(self.min_val, 3),
            "max": round(self.max_val, 3),
        }


@dataclass
class ProfileResult:
    """Benchmark results for a single profile (e.g., NS0, S0, S1)."""

    profile: str
    e2e_latency_ms: list[float] = field(default_factory=list)
    ttft_delay_ms: list[float] = field(default_factory=list)
    client_ttft_ms: list[float] = field(default_factory=list)
    cpu_time_ms: list[float] = field(default_factory=list)
    memory_peak_kb: list[float] = field(default_factory=list)
    throughput_rps: float = 0.0
    throughput_bytes_per_sec: float = 0.0
    total_response_bytes: int = 0

    def add_timestamps(self, ts: RequestTimestamps) -> None:
        self.e2e_latency_ms.append(ts.e2e_latency_ms)
        if ts.ttft_delay_ms > 0:
            self.ttft_delay_ms.append(ts.ttft_delay_ms)
        if ts.client_ttft_ms > 0:
            self.client_ttft_ms.append(ts.client_ttft_ms)

    def stats(self) -> dict:
        result = {
            "profile": self.profile,
            "e2e_latency_ms": LatencyStats.from_values(self.e2e_latency_ms).to_dict(),
        }
        if self.ttft_delay_ms:
            result["ttft_delay_ms"] = LatencyStats.from_values(
                self.ttft_delay_ms
            ).to_dict()
        if self.client_ttft_ms:
            result["client_ttft_ms"] = LatencyStats.from_values(
                self.client_ttft_ms
            ).to_dict()
        if self.cpu_time_ms:
            result["cpu_time_ms"] = LatencyStats.from_values(self.cpu_time_ms).to_dict()
        if self.memory_peak_kb:
            result["memory_peak_kb"] = LatencyStats.from_values(
                self.memory_peak_kb
            ).to_dict()
        if self.throughput_rps > 0:
            result["throughput_rps"] = round(self.throughput_rps, 2)
        if self.throughput_bytes_per_sec > 0:
            result["throughput_bytes_per_sec"] = round(self.throughput_bytes_per_sec, 2)
        return result


@dataclass
class OverheadResult:
    """Overhead comparison between guarded and baseline profiles."""

    guarded_profile: str
    baseline_profile: str
    absolute_overhead_ms: dict  # {p50, p95, p99}
    relative_overhead_pct: dict  # {p50, p95, p99}
    ttft_overhead_ms: dict | None = None

    def to_dict(self) -> dict:
        result = {
            "guarded": self.guarded_profile,
            "baseline": self.baseline_profile,
            "absolute_overhead_ms": self.absolute_overhead_ms,
            "relative_overhead_pct": self.relative_overhead_pct,
        }
        if self.ttft_overhead_ms:
            result["ttft_overhead_ms"] = self.ttft_overhead_ms
        return result


def compute_overhead(guarded: ProfileResult, baseline: ProfileResult) -> OverheadResult:
    """Compute absolute and relative overhead between guarded and baseline."""
    g = LatencyStats.from_values(guarded.e2e_latency_ms)
    b = LatencyStats.from_values(baseline.e2e_latency_ms)

    def safe_pct(guarded_val: float, baseline_val: float) -> float:
        if baseline_val == 0:
            return 0.0
        return round((guarded_val - baseline_val) / baseline_val * 100, 2)

    abs_overhead = {
        "p50": round(g.p50 - b.p50, 3),
        "p95": round(g.p95 - b.p95, 3),
        "p99": round(g.p99 - b.p99, 3),
    }
    rel_overhead = {
        "p50": safe_pct(g.p50, b.p50),
        "p95": safe_pct(g.p95, b.p95),
        "p99": safe_pct(g.p99, b.p99),
    }

    ttft_overhead = None
    if guarded.ttft_delay_ms and baseline.ttft_delay_ms:
        g_ttft = LatencyStats.from_values(guarded.ttft_delay_ms)
        b_ttft = LatencyStats.from_values(baseline.ttft_delay_ms)
        ttft_overhead = {
            "p50": round(g_ttft.p50 - b_ttft.p50, 3),
            "p95": round(g_ttft.p95 - b_ttft.p95, 3),
        }

    return OverheadResult(
        guarded_profile=guarded.profile,
        baseline_profile=baseline.profile,
        absolute_overhead_ms=abs_overhead,
        relative_overhead_pct=rel_overhead,
        ttft_overhead_ms=ttft_overhead,
    )


class BenchmarkTimer:
    """Context manager for precise timing."""

    def __init__(self) -> None:
        self.start = 0.0
        self.end = 0.0

    def __enter__(self):
        self.start = time.perf_counter()
        return self

    def __exit__(self, *args):
        self.end = time.perf_counter()

    @property
    def elapsed_ms(self) -> float:
        return (self.end - self.start) * 1000


def save_results(
    results: dict, output_dir: Path, filename: str = "results.json"
) -> Path:
    """Save benchmark results to JSON file."""
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / filename
    with open(output_path, "w") as f:
        json.dump(results, f, indent=2)
    return output_path


def print_summary_table(
    profiles: list[ProfileResult], overheads: list[OverheadResult]
) -> None:
    """Print ASCII summary table."""
    print("\n" + "=" * 90)
    print("LATENCY SUMMARY (ms)")
    print("=" * 90)
    print(
        f"{'Profile':<12} {'P50':>10} {'P95':>10} {'P99':>10} {'Mean':>10} {'StdDev':>10} {'N':>6}"
    )
    print("-" * 90)
    for pr in profiles:
        s = LatencyStats.from_values(pr.e2e_latency_ms)
        print(
            f"{pr.profile:<12} {s.p50:>10.2f} {s.p95:>10.2f} {s.p99:>10.2f} {s.mean:>10.2f} {s.stddev:>10.2f} {s.count:>6}"
        )

    if any(pr.ttft_delay_ms for pr in profiles):
        print("\n" + "=" * 90)
        print("TTFT DELAY (ms) — guardrail-induced first-visible-token delay")
        print("=" * 90)
        print(f"{'Profile':<12} {'P50':>10} {'P95':>10} {'P99':>10} {'Mean':>10}")
        print("-" * 90)
        for pr in profiles:
            if pr.ttft_delay_ms:
                s = LatencyStats.from_values(pr.ttft_delay_ms)
                print(
                    f"{pr.profile:<12} {s.p50:>10.2f} {s.p95:>10.2f} {s.p99:>10.2f} {s.mean:>10.2f}"
                )

    if overheads:
        print("\n" + "=" * 90)
        print("OVERHEAD vs MODE-MATCHED BASELINE")
        print("=" * 90)
        print(
            f"{'Guarded':<12} {'Baseline':<12} {'Abs P50':>10} {'Abs P95':>10} {'Rel P50':>10} {'Rel P95':>10}"
        )
        print("-" * 90)
        for o in overheads:
            print(
                f"{o.guarded_profile:<12} {o.baseline_profile:<12} "
                f"{o.absolute_overhead_ms['p50']:>10.2f} {o.absolute_overhead_ms['p95']:>10.2f} "
                f"{o.relative_overhead_pct['p50']:>9.1f}% {o.relative_overhead_pct['p95']:>9.1f}%"
            )

    print("=" * 90)
