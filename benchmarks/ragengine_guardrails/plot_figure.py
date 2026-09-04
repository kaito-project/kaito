#!/usr/bin/env python3
"""Generate overhead-vs-tokens figure for the blog."""

import json

import matplotlib

matplotlib.use("Agg")
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np

RESULTS_DIR = Path(__file__).resolve().parent / "results"


def main():
    with open(RESULTS_DIR / "overhead_vs_tokens.json") as f:
        d = json.load(f)

    rates = d["rates_us_per_token"]
    data = d["data"]

    fig, ax = plt.subplots(figsize=(8, 5))

    colors = ["#2563eb", "#f59e0b", "#ef4444"]
    markers = ["o", "s", "^"]

    for i, (label, series) in enumerate(data.items()):
        tokens = sorted(int(k) for k in series)
        overheads = [series[str(t)]["overhead_p50"] for t in tokens]
        rate = rates[label]

        ax.plot(
            tokens,
            overheads,
            color=colors[i],
            marker=markers[i],
            markersize=7,
            linewidth=2,
            label=f"{label}  ({rate} μs/tok)",
        )

        # Plot linear fit
        x_fit = np.linspace(min(tokens), max(tokens), 100)
        slope = rate / 1000  # μs/token → ms/token
        intercept = overheads[0] - slope * tokens[0]
        y_fit = slope * x_fit + intercept
        ax.plot(x_fit, y_fit, color=colors[i], linewidth=1, linestyle="--", alpha=0.4)

    ax.set_xlabel("Output tokens", fontsize=12)
    ax.set_ylabel("Guardrail overhead P50 (ms)", fontsize=12)
    ax.set_title(
        "Guardrail Processing Overhead vs Response Length",
        fontsize=13,
        fontweight="bold",
    )
    ax.legend(fontsize=10, loc="upper left")
    ax.grid(True, alpha=0.3)
    ax.set_xlim(0, 17000)
    ax.set_ylim(0, None)

    plt.tight_layout()

    out_path = RESULTS_DIR / "overhead_vs_tokens.png"
    fig.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Figure saved to {out_path}")

    # Also save SVG for blog
    svg_path = RESULTS_DIR / "overhead_vs_tokens.svg"
    fig.savefig(svg_path, bbox_inches="tight")
    print(f"SVG saved to {svg_path}")


if __name__ == "__main__":
    main()
