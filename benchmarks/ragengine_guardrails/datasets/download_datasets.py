#!/usr/bin/env python3
"""Download public datasets for e2e guardrails benchmarking.

Datasets:
  - ALERT (Babelscape/ALERT): 15K red-team prompts, 6 categories
  - Do-Not-Answer (LibrAI/do-not-answer): 939 safety-boundary prompts
  - ai4privacy/pii-masking-400k: 400K PII-annotated entries
  - WildGuardMix (allenai/wildguardmix): 90K safety-labeled prompt-response pairs

Usage:
    pip install datasets
    python -m benchmarks.ragengine_guardrails.datasets.download_datasets
"""

import json
import sys
from pathlib import Path

DATASETS_DIR = Path(__file__).resolve().parent


def download_alert():
    """Download ALERT dataset from HuggingFace."""
    from datasets import load_dataset

    print("Downloading ALERT dataset...")
    ds = load_dataset("Babelscape/ALERT", "alert", split="test")

    output = DATASETS_DIR / "alert_prompts.jsonl"
    count = 0
    with open(output, "w") as f:
        for row in ds:
            entry = {
                "prompt": row.get("prompt", ""),
                "category": row.get("category", "unknown"),
                "source": "ALERT",
            }
            if entry["prompt"].strip():
                f.write(json.dumps(entry, ensure_ascii=False) + "\n")
                count += 1

    print(f"  Saved {count} prompts to {output}")
    return count


def download_do_not_answer():
    """Download Do-Not-Answer dataset from HuggingFace."""
    from datasets import load_dataset

    print("Downloading Do-Not-Answer dataset...")
    ds = load_dataset("LibrAI/do-not-answer", split="train")

    output = DATASETS_DIR / "do_not_answer_prompts.jsonl"
    count = 0
    with open(output, "w") as f:
        for row in ds:
            prompt = row.get("question", "") or row.get("prompt", "")
            entry = {
                "prompt": prompt,
                "category": row.get("harm_area", row.get("types_of_harm", "unknown")),
                "source": "Do-Not-Answer",
            }
            if entry["prompt"].strip():
                f.write(json.dumps(entry, ensure_ascii=False) + "\n")
                count += 1

    print(f"  Saved {count} prompts to {output}")
    return count


def download_ai4privacy(max_samples=1000):
    """Stream-sample ai4privacy/pii-masking-400k from HuggingFace."""
    import random as _rng

    from datasets import load_dataset

    print(
        f"Streaming ai4privacy/pii-masking-400k (sampling {max_samples} English entries)..."
    )
    ds = load_dataset("ai4privacy/pii-masking-400k", split="train", streaming=True)

    pool = []
    seen = 0
    for row in ds:
        lang = row.get("language", "")
        source_text = row.get("source_text", "")
        if not lang.startswith("en") or not source_text or len(source_text) < 50:
            continue
        seen += 1
        if len(pool) < max_samples:
            pool.append(row)
        else:
            j = _rng.randint(0, seen - 1)
            if j < max_samples:
                pool[j] = row

    output = DATASETS_DIR / "ai4privacy_prompts.jsonl"
    with open(output, "w") as f:
        for row in pool:
            entry = {
                "source_text": row.get("source_text", ""),
                "masked_text": row.get("masked_text", ""),
                "privacy_mask": row.get("privacy_mask", {}),
                "language": row.get("language", "unknown"),
                "locale": row.get("locale", "unknown"),
                "source": "ai4privacy",
            }
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    print(f"  Saved {len(pool)} entries to {output} (from {seen} English candidates)")
    return len(pool)


def download_wildguardmix(max_samples=500):
    """Stream-sample benign prompts from WildGuardMix."""
    import random as _rng

    from datasets import load_dataset

    print(f"Streaming WildGuardMix (sampling {max_samples} entries)...")
    ds = load_dataset(
        "allenai/wildguardmix", "wildguardtrain", split="train", streaming=True
    )

    pool = []
    seen = 0
    for row in ds:
        prompt = row.get("prompt", "")
        if not prompt.strip():
            continue
        seen += 1
        if len(pool) < max_samples:
            pool.append(row)
        else:
            j = _rng.randint(0, seen - 1)
            if j < max_samples:
                pool[j] = row

    output = DATASETS_DIR / "wildguardmix_prompts.jsonl"
    with open(output, "w") as f:
        for row in pool:
            entry = {
                "prompt": row.get("prompt", ""),
                "response": row.get("response", ""),
                "prompt_harm_label": row.get("prompt_harm_label", ""),
                "response_harm_label": row.get("response_harm_label", ""),
                "harm_category": row.get("harm_category", ""),
                "source": "WildGuardMix",
            }
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    print(f"  Saved {len(pool)} entries to {output} (from {seen} candidates)")
    return len(pool)


def main():
    print("=== Downloading Public Datasets ===\n")

    try:
        import datasets  # noqa: F401
    except ImportError:
        print("ERROR: 'datasets' package not installed. Run: pip install datasets")
        sys.exit(1)

    total = 0
    total += download_alert()
    total += download_do_not_answer()
    total += download_ai4privacy()
    try:
        total += download_wildguardmix()
    except Exception as e:
        print(f"  SKIPPED WildGuardMix: {e}")

    print(f"\nTotal: {total} entries downloaded")
    print("Next: run curate_prompts.py to sample and build benchmark_prompts_v2.jsonl")


if __name__ == "__main__":
    main()
