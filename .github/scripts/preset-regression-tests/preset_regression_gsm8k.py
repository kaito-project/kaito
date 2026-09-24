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

"""Run bounded GSM8K against a deployed OpenAI-compatible endpoint."""

from __future__ import annotations

import argparse
import json
import re
import signal
import time
import urllib.request
from pathlib import Path
from typing import Any

import yaml
from preset_regression_benchmarks import (
    compare_accuracy,
    find_baseline,
    load_yaml,
    related_baselines,
    resolve_profile,
    target_policy_key,
    validate_gsm8k_data,
)


def extract_accuracy(results: dict[str, Any], task: str, metric: str) -> float:
    value = results.get("results", {}).get(task, {}).get(metric)
    if not isinstance(value, (int, float)):
        raise ValueError(f"lm-eval result for {task!r} has no {metric!r} metric")
    return float(value)


def responses_by_document(results: dict[str, Any], task: str) -> dict[Any, list[str]]:
    responses: dict[Any, list[str]] = {}
    for sample in results.get("samples", {}).get(task, []):
        doc_id = sample.get("doc_id")
        flattened = [
            response
            for batch in sample.get("resps", [])
            if isinstance(batch, list)
            for response in batch
            if isinstance(response, str)
        ]
        responses.setdefault(doc_id, flattened)
    return responses


def failed_samples(
    results: dict[str, Any], task: str, metric: str
) -> list[dict[str, Any]]:
    selected_filter = metric.split(",", maxsplit=1)[-1]
    failures: list[dict[str, Any]] = []
    for sample in results.get("samples", {}).get(task, []):
        if sample.get("filter") != selected_filter:
            continue
        score = sample.get("exact_match")
        if isinstance(score, (int, float)) and score > 0:
            continue

        responses = [
            response
            for batch in sample.get("resps", [])
            if isinstance(batch, list)
            for response in batch
            if isinstance(response, str)
        ]
        response = responses[0] if responses else ""
        filtered = sample.get("filtered_resps", [])
        extracted = filtered[0] if filtered else "[invalid]"
        target = str(sample.get("target", ""))
        expected_match = re.search(r"####\s*(-?[$0-9.,]+)", target)
        expected = expected_match.group(1) if expected_match else target

        if not response.strip():
            reason = "empty-response"
        elif extracted == "[invalid]":
            reason = "answer-extraction-failed"
        else:
            reason = "exact-match-failed"

        failures.append(
            {
                "docId": sample.get("doc_id"),
                "question": sample.get("doc", {}).get("question", ""),
                "expectedAnswer": expected,
                "extractedAnswer": extracted,
                "reason": reason,
                "responseTail": response[-1000:],
            }
        )
    return failures


def print_failed_samples(failures: list[dict[str, Any]]) -> None:
    for failure in failures:
        print("GSM8K_FAILED_SAMPLE " + json.dumps(failure, sort_keys=True))


def effective_max_gen_tokens(
    endpoint: str, configured_max: int, prompt_token_reserve: int
) -> int:
    models_endpoint = endpoint.removesuffix("/v1/chat/completions") + "/v1/models"
    with urllib.request.urlopen(models_endpoint, timeout=30) as response:
        payload = json.load(response)
    models = payload.get("data", [])
    if not models:
        raise ValueError("/v1/models returned no served models")
    model_limit = int(models[0].get("max_model_len") or configured_max)
    available_output_tokens = model_limit - prompt_token_reserve
    if model_limit <= 0 or available_output_tokens <= 0:
        raise ValueError("/v1/models returned an invalid max_model_len")
    return min(configured_max, available_output_tokens)


def run_evaluation(
    served_model: str,
    endpoint: str,
    benchmark: dict[str, Any],
    profile: dict[str, Any],
    num_concurrent: int,
    request_timeout: int,
    max_retries: int,
    max_gen_tokens: int,
) -> dict[str, Any]:
    import lm_eval.tasks
    from lm_eval import evaluator

    sample_count = int(benchmark["sampleSelection"]["count"])
    task_path = Path(lm_eval.tasks.__file__).parent / "gsm8k/gsm8k.yaml"
    task_config = yaml.safe_load(task_path.read_text(encoding="utf-8"))
    task_config["dataset_kwargs"] = {"revision": benchmark["datasetRevision"]}
    return evaluator.simple_evaluate(
        model="local-chat-completions",
        model_args={
            "model": served_model,
            "base_url": endpoint,
            "num_concurrent": num_concurrent,
            "max_retries": max_retries,
            "timeout": request_timeout,
            "max_gen_toks": max_gen_tokens,
            "tokenizer_backend": "none",
            "tokenized_requests": False,
        },
        tasks=[task_config],
        limit=sample_count,
        bootstrap_iters=0,
        log_samples=True,
        apply_chat_template=bool(profile["applyChatTemplate"]),
        fewshot_as_multiturn=bool(profile["fewshotAsMultiturn"]),
        gen_kwargs={
            "chat_template_kwargs": profile.get("chatTemplateKwargs", {}),
            "until": profile.get("stopSequences", []),
            "temperature": float(profile["temperature"]),
        },
        random_seed=0,
        numpy_random_seed=1234,
        torch_random_seed=1234,
        fewshot_random_seed=1234,
    )


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(value, indent=2, sort_keys=True, default=str) + "\n",
        encoding="utf-8",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True)
    parser.add_argument("--served-model", required=True)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--instance-type", required=True)
    parser.add_argument("--nodes", required=True, type=int)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--baselines", required=True, type=Path)
    parser.add_argument("--summary-output", required=True, type=Path)
    parser.add_argument("--raw-output", required=True, type=Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    started = time.monotonic()
    config = load_yaml(args.config)
    baselines = load_yaml(args.baselines)
    validate_gsm8k_data(config, baselines)
    profile_name, profile = resolve_profile(config, args.model)
    benchmark = config["benchmark"]
    execution = config["execution"]
    comparison_policy = config["comparison"]
    configured_max_gen_tokens = int(profile["maxGenTokens"])
    max_gen_tokens = effective_max_gen_tokens(
        args.endpoint,
        configured_max_gen_tokens,
        int(benchmark["promptTokenReserve"]),
    )
    baseline = find_baseline(baselines, args.model, args.instance_type, args.nodes)
    if baseline is None:
        related = related_baselines(baselines, args.model, args.instance_type)
        if related:
            summary = {
                "model": args.model,
                "instanceType": args.instance_type,
                "nodes": args.nodes,
                "profile": profile_name,
                "status": "baseline-config-mismatch",
                "passed": False,
                "actualNodes": args.nodes,
                "baselineNodes": sorted(int(item["nodes"]) for item in related),
                "durationSeconds": round(time.monotonic() - started, 3),
            }
            write_json(args.summary_output, summary)
            print(json.dumps(summary, sort_keys=True))
            return 1
    elif baseline.get("profile") != profile_name:
        summary = {
            "model": args.model,
            "instanceType": args.instance_type,
            "nodes": args.nodes,
            "profile": profile_name,
            "baselineProfile": baseline.get("profile"),
            "status": "baseline-config-mismatch",
            "passed": False,
            "durationSeconds": round(time.monotonic() - started, 3),
        }
        write_json(args.summary_output, summary)
        print(json.dumps(summary, sort_keys=True))
        return 1

    def deadline(*_: Any) -> None:
        raise TimeoutError(f"GSM8K exceeded {int(execution['timeoutSeconds'])} seconds")

    signal.signal(signal.SIGALRM, deadline)
    signal.alarm(int(execution["timeoutSeconds"]))
    try:
        raw_results = run_evaluation(
            args.served_model,
            args.endpoint,
            benchmark,
            profile,
            int(execution["numConcurrent"]),
            int(execution["requestTimeoutSeconds"]),
            int(execution["maxRetries"]),
            max_gen_tokens,
        )
        write_json(args.raw_output, raw_results)
        accuracy = extract_accuracy(raw_results, benchmark["task"], benchmark["metric"])
        responses = responses_by_document(raw_results, benchmark["task"])
        failures = failed_samples(raw_results, benchmark["task"], benchmark["metric"])
        print_failed_samples(failures)
        empty_responses = sum(
            not values or all(not value.strip() for value in values)
            for values in responses.values()
        )
        if empty_responses:
            raise ValueError(
                f"{empty_responses} of {len(responses)} responses were empty; "
                "increase the profile generation budget or inspect response parsing"
            )
        comparison = compare_accuracy(
            accuracy,
            baseline,
            profile_name,
            float(
                comparison_policy.get("maxRegressionOverrides", {}).get(
                    target_policy_key(args.model, args.instance_type, args.nodes),
                    comparison_policy["defaultMaxRegression"],
                )
            ),
            bool(comparison_policy["requireBaselines"]),
        )
        summary = {
            "model": args.model,
            "servedModel": args.served_model,
            "instanceType": args.instance_type,
            "nodes": args.nodes,
            "profile": profile_name,
            "configuredMaxGenTokens": configured_max_gen_tokens,
            "maxGenTokens": max_gen_tokens,
            "evaluated": len(responses),
            "correct": round(accuracy * len(responses)),
            "accuracy": accuracy,
            "metric": benchmark["metric"],
            "emptyResponses": empty_responses,
            "failedSampleCount": len(failures),
            "failedSamples": failures,
            "durationSeconds": round(time.monotonic() - started, 3),
            **comparison,
        }
    except Exception as error:  # noqa: BLE001 - persist evaluator failures for CI
        summary = {
            "model": args.model,
            "instanceType": args.instance_type,
            "nodes": args.nodes,
            "profile": profile_name,
            "status": "correctness-invalid",
            "passed": False,
            "durationSeconds": round(time.monotonic() - started, 3),
            "error": f"{type(error).__name__}: {error}",
        }
        write_json(args.summary_output, summary)
        raise
    finally:
        signal.alarm(0)

    write_json(args.summary_output, summary)
    print(json.dumps(summary, sort_keys=True))
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
