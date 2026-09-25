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
import asyncio
import json
import logging
import re
import signal
import time
import urllib.request
from collections.abc import Awaitable, Callable
from pathlib import Path
from typing import Any

import yaml
from preset_regression_benchmarks import (
    compare_accuracy,
    find_baseline,
    load_yaml,
    related_baselines,
    resolve_gsm8k_execution,
    resolve_profile,
    target_policy_key,
    validate_gsm8k_data,
)

LOGGER = logging.getLogger(__name__)


class EvaluationDeadlineExceeded(Exception):
    pass


def should_retry_api_error(
    status: int | None, *, is_connection_error: bool, is_timeout: bool
) -> bool:
    if status is not None:
        return status == 429 or status >= 500
    return is_connection_error and not is_timeout


async def request_or_empty(
    request: Awaitable[list[str]],
    response_count: int,
    record_timeouts: Callable[[int], None],
) -> list[str]:
    try:
        return await request
    except TimeoutError as error:
        record_timeouts(response_count)
        LOGGER.warning(
            "GSM8K request timed out and will be scored incorrect: %r", error
        )
        return [""] * response_count


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


def generation_kwargs(profile: dict[str, Any]) -> dict[str, Any]:
    kwargs = {
        "until": profile.get("stopSequences", []),
        "temperature": float(profile["temperature"]),
        **profile.get("requestKwargs", {}),
    }
    if "chatTemplateKwargs" in profile:
        kwargs["chat_template_kwargs"] = profile["chatTemplateKwargs"]
    return kwargs


def run_evaluation(
    served_model: str,
    endpoint: str,
    benchmark: dict[str, Any],
    profile: dict[str, Any],
    num_concurrent: int,
    request_timeout: int,
    max_retries: int,
    max_gen_tokens: int,
) -> tuple[dict[str, Any], int]:
    import lm_eval.tasks
    from aiohttp import (
        ClientConnectionError,
        ClientSession,
        ClientTimeout,
        TCPConnector,
    )
    from lm_eval import evaluator
    from lm_eval.models.openai_completions import LocalChatCompletion
    from lm_eval.models.utils import chunks
    from tenacity import (
        retry,
        retry_if_exception,
        stop_after_attempt,
        wait_exponential,
    )
    from tqdm.asyncio import tqdm_asyncio

    class TimeoutTolerantLocalChatCompletion(LocalChatCompletion):
        def __init__(self, **kwargs: Any) -> None:
            super().__init__(**kwargs)
            self.request_timeout_count = 0

        def record_timeouts(self, count: int) -> None:
            self.request_timeout_count += count

        async def get_batched_requests(
            self,
            requests: list[Any],
            cache_keys: list[Any],
            *,
            generate: bool = True,
            ctxlens: list[int] | None = None,
            **kwargs: Any,
        ) -> list[list[str]]:
            ctxlens = ctxlens or [None] * len(requests)
            connector = TCPConnector(
                limit=self._concurrent, ssl=self.verify_certificate
            )
            semaphore = asyncio.Semaphore(self._concurrent)
            async with ClientSession(
                connector=connector, timeout=ClientTimeout(total=self.timeout)
            ) as session:
                retry_request = retry(
                    stop=stop_after_attempt(self.max_retries),
                    wait=wait_exponential(multiplier=0.5, min=1, max=10),
                    retry=retry_if_exception(
                        lambda error: should_retry_api_error(
                            getattr(error, "status", None),
                            is_connection_error=isinstance(
                                error, ClientConnectionError
                            ),
                            is_timeout=isinstance(error, TimeoutError),
                        )
                    ),
                    reraise=True,
                )(self.amodel_call)
                tasks = []
                for messages, keys, lengths in zip(
                    chunks(requests, n=self._batch_size),
                    chunks(cache_keys, n=self._batch_size),
                    chunks(ctxlens, n=self._batch_size),
                    strict=False,
                ):
                    tasks.append(
                        asyncio.create_task(
                            request_or_empty(
                                retry_request(
                                    session=session,
                                    sem=semaphore,
                                    messages=messages,
                                    cache_keys=keys,
                                    generate=generate,
                                    ctxlens=lengths,
                                    **kwargs,
                                ),
                                response_count=len(messages),
                                record_timeouts=self.record_timeouts,
                            )
                        )
                    )
                return await tqdm_asyncio.gather(*tasks, desc="Requesting API")

    sample_count = int(benchmark["sampleSelection"]["count"])
    task_path = Path(lm_eval.tasks.__file__).parent / "gsm8k/gsm8k.yaml"
    task_config = yaml.safe_load(task_path.read_text(encoding="utf-8"))
    task_config["dataset_kwargs"] = {"revision": benchmark["datasetRevision"]}
    model = TimeoutTolerantLocalChatCompletion(
        model=served_model,
        base_url=endpoint,
        num_concurrent=num_concurrent,
        max_retries=max_retries,
        timeout=request_timeout,
        max_gen_toks=max_gen_tokens,
        tokenizer_backend="none",
        tokenized_requests=False,
    )
    results = evaluator.simple_evaluate(
        model=model,
        tasks=[task_config],
        limit=sample_count,
        bootstrap_iters=0,
        log_samples=True,
        apply_chat_template=bool(profile["applyChatTemplate"]),
        fewshot_as_multiturn=bool(profile["fewshotAsMultiturn"]),
        gen_kwargs=generation_kwargs(profile),
        random_seed=0,
        numpy_random_seed=1234,
        torch_random_seed=1234,
        fewshot_random_seed=1234,
    )
    return results, model.request_timeout_count


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
    execution = resolve_gsm8k_execution(config, args.model)
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
        raise EvaluationDeadlineExceeded(
            f"GSM8K exceeded {int(execution['timeoutSeconds'])} seconds"
        )

    signal.signal(signal.SIGALRM, deadline)
    signal.alarm(int(execution["timeoutSeconds"]))
    try:
        raw_results, request_timeouts = run_evaluation(
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
        # lm-eval already scores empty final responses as incorrect. Keep their
        # count and diagnostics without discarding the otherwise valid run.
        empty_responses = sum(
            not values or all(not value.strip() for value in values)
            for values in responses.values()
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
            "requestTimeouts": request_timeouts,
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
