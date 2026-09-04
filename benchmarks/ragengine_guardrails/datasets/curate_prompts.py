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

#!/usr/bin/env python3
"""Curate benchmark prompts v2 from public + custom datasets.

Maximizes public dataset usage for reproducibility.
Custom prompts only where no public dataset exists.

Usage:
    python -m benchmarks.ragengine_guardrails.datasets.curate_prompts
"""

import json
import random
from pathlib import Path

DATASETS_DIR = Path(__file__).resolve().parent
RANDOM_SEED = 42

TARGETS = {
    "toxicity": 80,
    "refusal": 60,
    "pii": 100,
    "clean": 60,
    "secrets": 40,
    "ban_substrings": 5,
    "json": 15,
    "reading_time": 15,
    "secrets_targeted": 5,
}


def load_jsonl(path: Path) -> list[dict]:
    if not path.exists():
        return []
    entries = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                entries.append(json.loads(line))
    return entries


def load_json_array(path: Path) -> list[dict]:
    if not path.exists():
        return []
    with open(path) as f:
        data = json.load(f)
    if isinstance(data, list):
        return data
    return [data]


def curate_pii_from_ai4privacy(count: int) -> list[dict]:
    """Sample PII-rich entries from ai4privacy and convert to prompts."""
    entries = load_jsonl(DATASETS_DIR / "ai4privacy_prompts.jsonl")
    if not entries:
        print(
            "  WARNING: ai4privacy_prompts.jsonl not found. Run download_datasets.py."
        )
        return []

    en_entries = [e for e in entries if e.get("language", "").startswith("en")]
    if len(en_entries) < count:
        en_entries = entries

    sampled = random.sample(en_entries, min(count, len(en_entries)))

    result = []
    pii_prompt_templates = [
        "Rewrite the following text, keeping all details intact:\n\n{text}",
        "Summarize this passage while preserving all specific details:\n\n{text}",
        "Convert this into a professional report format:\n\n{text}",
        "Please proofread and improve the following text:\n\n{text}",
        "Rephrase this content in a more formal tone:\n\n{text}",
    ]
    for i, entry in enumerate(sampled):
        template = pii_prompt_templates[i % len(pii_prompt_templates)]
        prompt = template.format(text=entry["source_text"][:1500])
        result.append(
            {
                "prompt": prompt,
                "category": "pii",
                "source": "ai4privacy",
                "bench_category": "pii",
            }
        )

    print(f"  ai4privacy: sampled {len(result)} PII prompts (English)")
    return result


def curate_clean_from_wildguardmix(count: int) -> list[dict]:
    """Sample benign prompts from WildGuardMix."""
    entries = load_jsonl(DATASETS_DIR / "wildguardmix_prompts.jsonl")
    if not entries:
        print(
            "  WARNING: wildguardmix_prompts.jsonl not found. Run download_datasets.py."
        )
        return []

    benign = [e for e in entries if e.get("prompt_harm_label", "") == "unharmful"]
    if not benign:
        benign = entries

    sampled = random.sample(benign, min(count, len(benign)))

    result = []
    for entry in sampled:
        result.append(
            {
                "prompt": entry["prompt"],
                "category": "clean",
                "source": "WildGuardMix",
                "bench_category": "clean",
            }
        )

    print(f"  WildGuardMix: sampled {len(result)} benign/clean prompts")
    return result


def curate():
    random.seed(RANDOM_SEED)

    result = []

    # --- Public datasets ---

    # ALERT: toxicity
    alert = load_jsonl(DATASETS_DIR / "alert_prompts.jsonl")
    if alert:
        sampled = random.sample(alert, min(TARGETS["toxicity"], len(alert)))
        for entry in sampled:
            entry["bench_category"] = "toxicity"
        result.extend(sampled)
        print(f"  ALERT: sampled {len(sampled)} toxicity prompts")
    else:
        print("  WARNING: alert_prompts.jsonl not found.")

    # Do-Not-Answer: refusal
    dna = load_jsonl(DATASETS_DIR / "do_not_answer_prompts.jsonl")
    if dna:
        sampled = random.sample(dna, min(TARGETS["refusal"], len(dna)))
        for entry in sampled:
            entry["bench_category"] = "refusal"
        result.extend(sampled)
        print(f"  Do-Not-Answer: sampled {len(sampled)} refusal prompts")
    else:
        print("  WARNING: do_not_answer_prompts.jsonl not found.")

    # ai4privacy: PII
    pii_prompts = curate_pii_from_ai4privacy(TARGETS["pii"])
    result.extend(pii_prompts)

    # WildGuardMix: clean/benign (or fallback to custom clean)
    clean_prompts = curate_clean_from_wildguardmix(TARGETS["clean"])
    if not clean_prompts:
        clean_data = load_json_array(DATASETS_DIR / "clean_prompts.jsonl")
        for entry in clean_data[: TARGETS["clean"]]:
            entry["bench_category"] = "clean"
            entry["source"] = "custom"
        clean_prompts = clean_data[: TARGETS["clean"]]
        print(f"  Custom Clean (fallback): {len(clean_prompts)} prompts")
    result.extend(clean_prompts)

    # --- Custom datasets (no public alternative) ---

    custom_pii = load_json_array(DATASETS_DIR / "custom_pii_prompts.jsonl")
    secrets_prompts = [e for e in custom_pii if e.get("category") == "secrets"]
    for entry in secrets_prompts[: TARGETS["secrets"]]:
        entry["bench_category"] = "secrets"
    result.extend(secrets_prompts[: TARGETS["secrets"]])
    print(f"  Custom Secrets: {min(TARGETS['secrets'], len(secrets_prompts))} prompts")

    # Scanner-targeted
    scanner_targeted = load_jsonl(DATASETS_DIR / "scanner_targeted_prompts.jsonl")
    if not scanner_targeted:
        scanner_targeted = load_json_array(
            DATASETS_DIR / "scanner_targeted_prompts.jsonl"
        )

    for cat in ["ban_substrings", "json", "reading_time", "secrets_targeted"]:
        cat_prompts = [e for e in scanner_targeted if e.get("category") == cat]
        target = TARGETS.get(cat, 10)
        selected = cat_prompts[:target]
        for entry in selected:
            entry["bench_category"] = cat
        result.extend(selected)
        print(f"  Scanner-targeted ({cat}): {len(selected)} prompts")

    random.shuffle(result)

    output = DATASETS_DIR / "benchmark_prompts_v2.jsonl"
    with open(output, "w") as f:
        for entry in result:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    print(f"\nTotal: {len(result)} prompts → {output}")

    categories = {}
    sources = {}
    for entry in result:
        cat = entry.get("bench_category", "unknown")
        src = entry.get("source", "unknown")
        categories[cat] = categories.get(cat, 0) + 1
        sources[src] = sources.get(src, 0) + 1
    print("\nBy category:", json.dumps(categories, indent=2))
    print("\nBy source:", json.dumps(sources, indent=2))

    public_count = sum(
        v
        for k, v in sources.items()
        if k in ("ALERT", "Do-Not-Answer", "ai4privacy", "WildGuardMix")
    )
    print(
        f"\nPublic: {public_count}/{len(result)} ({100 * public_count / len(result):.0f}%)"
    )


if __name__ == "__main__":
    print("=== Curating Benchmark Prompts v2 ===\n")
    curate()
