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
"""
MT-Bench Evaluation for Kaito Workspaces

Evaluates models deployed via Kaito using the MT-Bench benchmark (80 multi-turn
questions across 8 categories). Uses a strong LLM as the judge.

Three-stage pipeline:
  1. Generate model answers by calling the Kaito workspace's /v1/chat/completions
  2. Judge answers using strong LLM (single-answer grading, 1-10)
  3. Aggregate scores and emit structured results

Environment Variables:
  KAITO_ENDPOINT          Workspace inference URL (required)
  MODEL_NAME              Model name for chat completions (required)
  JUDGE_SOURCE            Source of LLM judge (required, e.g. MICROSOFT_AI_FOUNDRY)
  JUDGE_ENDPOINT          Judge LLM endpoint URL (required)
  JUDGE_API_KEY           Judge LLM API key (required)
  AZURE_OPENAI_DEPLOYMENT Deployment name (required when JUDGE_SOURCE=MICROSOFT_AI_FOUNDRY)
  AZURE_OPENAI_API_VERSION API version (required when JUDGE_SOURCE=MICROSOFT_AI_FOUNDRY)
  PARALLEL                Concurrent judge API calls (default: 2)
  MAX_TOKENS              Max tokens for model responses (default: 1024)
  TEMPERATURE             Model temperature (default: 0.0)
  DRY_RUN                 If "true", skip judging stage (default: false)
  RESULTS_DIR             Directory for detailed output files (default: /results)

Output:
  Structured log line to stdout:
    KAITO_MTBENCH_RESULT <RFC3339> <JSON>
  Detailed per-question files in RESULTS_DIR:
    mt_bench_answers.jsonl
    mt_bench_judgments.jsonl
    mt_bench_report.json
"""

import concurrent.futures
import json
import logging
import os
import re
import sys
import time
from pathlib import Path
from typing import Any

from huggingface_hub import hf_hub_download
from openai import AzureOpenAI, OpenAI

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
)
log = logging.getLogger("mt_bench_eval")

# ── MT-Bench data source (downloaded from HuggingFace at runtime) ─────────────

HF_REPO_ID = "lmsys/mt-bench"
HF_REPO_TYPE = "space"
HF_QUESTIONS_PATH = "data/mt_bench/question.jsonl"

DATA_DIR = Path(os.environ.get("DATA_DIR", "/app/data"))

# ── Judge prompt templates ────────

JUDGE_PROMPTS: dict[str, dict] = {
    "single-v1": {
        "name": "single-v1",
        "type": "single",
        "system_prompt": "You are a helpful assistant.",
        "prompt_template": (
            "[Instruction]\n"
            "Please act as an impartial judge and evaluate the quality of the "
            "response provided by an AI assistant to the user question displayed "
            "below. Your evaluation should consider factors such as the helpfulness, "
            "relevance, accuracy, depth, creativity, and level of detail of the "
            "response. Begin your evaluation by providing a short explanation. Be as "
            "objective as possible. After providing your explanation, you must rate "
            "the response on a scale of 1 to 10 by strictly following this format: "
            '"[[rating]]", for example: "Rating: [[5]]".\n'
            "\n"
            "[Question]\n"
            "{question}\n"
            "\n"
            "[The Start of Assistant's Answer]\n"
            "{answer}\n"
            "[The End of Assistant's Answer]"
        ),
        "output_format": "[[rating]]",
    },
    "single-math-v1": {
        "name": "single-math-v1",
        "type": "single",
        "system_prompt": "You are a helpful assistant.",
        "prompt_template": (
            "[Instruction]\n"
            "Please act as an impartial judge and evaluate the quality of the "
            "response provided by an AI assistant to the user question displayed "
            "below. Your evaluation should consider correctness and helpfulness. "
            "You will be given a reference answer and the assistant's answer. Begin "
            "your evaluation by comparing the assistant's answer with the reference "
            "answer. Identify and correct any mistakes. Be as objective as possible. "
            "After providing your explanation, you must rate the response on a scale "
            'of 1 to 10 by strictly following this format: "[[rating]]", for '
            'example: "Rating: [[5]]".\n'
            "\n"
            "[Question]\n"
            "{question}\n"
            "\n"
            "[The Start of Reference Answer]\n"
            "{ref_answer_1}\n"
            "[The End of Reference Answer]\n"
            "\n"
            "[The Start of Assistant's Answer]\n"
            "{answer}\n"
            "[The End of Assistant's Answer]"
        ),
        "output_format": "[[rating]]",
    },
    "single-v1-multi-turn": {
        "name": "single-v1-multi-turn",
        "type": "single",
        "system_prompt": (
            "Please act as an impartial judge and evaluate the quality of the "
            "response provided by an AI assistant to the user question displayed "
            "below. Your evaluation should consider factors such as the helpfulness, "
            "relevance, accuracy, depth, creativity, and level of detail of the "
            "response. You evaluation should focus on the assistant's answer to the "
            "second user question. Begin your evaluation by providing a short "
            "explanation. Be as objective as possible. After providing your "
            "explanation, you must rate the response on a scale of 1 to 10 by "
            'strictly following this format: "[[rating]]", for example: '
            '"Rating: [[5]]".\n\n'
        ),
        "prompt_template": (
            "<|The Start of Assistant A's Conversation with User|>\n"
            "\n"
            "### User:\n"
            "{question_1}\n"
            "\n"
            "### Assistant A:\n"
            "{answer_1}\n"
            "\n"
            "### User:\n"
            "{question_2}\n"
            "\n"
            "### Assistant A:\n"
            "{answer_2}\n"
            "\n"
            "<|The End of Assistant A's Conversation with User|>"
        ),
        "output_format": "[[rating]]",
    },
    "single-math-v1-multi-turn": {
        "name": "single-math-v1-multi-turn",
        "type": "single",
        "system_prompt": (
            "Please act as an impartial judge and evaluate the quality of the "
            "response provided by an AI assistant to the user question. Your "
            "evaluation should consider correctness and helpfulness. You will be "
            "given a reference answer and the assistant's answer. You evaluation "
            "should focus on the assistant's answer to the second question. Begin "
            "your evaluation by comparing the assistant's answer with the reference "
            "answer. Identify and correct any mistakes. Be as objective as possible. "
            "After providing your explanation, you must rate the response on a scale "
            'of 1 to 10 by strictly following this format: "[[rating]]", for '
            'example: "Rating: [[5]]".\n\n'
        ),
        "prompt_template": (
            "<|The Start of Reference Answer|>\n"
            "\n"
            "### User:\n"
            "{question_1}\n"
            "\n"
            "### Reference answer:\n"
            "{ref_answer_1}\n"
            "\n"
            "### User:\n"
            "{question_2}\n"
            "\n"
            "### Reference answer:\n"
            "{ref_answer_2}\n"
            "\n"
            "<|The End of Reference Answer|>\n"
            "\n"
            "\n"
            "<|The Start of Assistant A's Conversation with User|>\n"
            "\n"
            "### User:\n"
            "{question_1}\n"
            "\n"
            "### Assistant A:\n"
            "{answer_1}\n"
            "\n"
            "### User:\n"
            "{question_2}\n"
            "\n"
            "### Assistant A:\n"
            "{answer_2}\n"
            "\n"
            "<|The End of Assistant A's Conversation with User|>"
        ),
        "output_format": "[[rating]]",
    },
}

# ── Configuration ────────────────────────────────────────────────────────────


def _env(name: str, default: str | None = None, required: bool = False) -> str:
    val = os.environ.get(name, default)
    if required and not val:
        log.error("Required environment variable %s is not set", name)
        sys.exit(1)
    return val  # type: ignore[return-value]


SUPPORTED_JUDGE_SOURCES = {"MICROSOFT_AI_FOUNDRY"}


def load_config() -> dict:
    judge_source = _env("JUDGE_SOURCE", required=True)
    if judge_source not in SUPPORTED_JUDGE_SOURCES:
        log.error(
            "Unsupported JUDGE_SOURCE=%s. Supported values: %s",
            judge_source,
            ", ".join(sorted(SUPPORTED_JUDGE_SOURCES)),
        )
        sys.exit(1)

    cfg = {
        "kaito_endpoint": _env("KAITO_ENDPOINT", required=True).rstrip("/"),
        "model_name": _env("MODEL_NAME", required=True),
        "judge_source": judge_source,
        "judge_endpoint": _env("JUDGE_ENDPOINT", required=True),
        "judge_api_key": _env("JUDGE_API_KEY", required=True),
        "parallel": int(_env("PARALLEL", "2")),
        "max_tokens": int(_env("MAX_TOKENS", "1024")),
        "temperature": float(_env("TEMPERATURE", "0.0")),
        "dry_run": _env("DRY_RUN", "false").lower() == "true",
        "results_dir": Path(_env("RESULTS_DIR", "/results")),
    }

    if judge_source == "MICROSOFT_AI_FOUNDRY":
        cfg["azure_deployment"] = _env("AZURE_OPENAI_DEPLOYMENT", required=True)
        cfg["azure_api_version"] = _env("AZURE_OPENAI_API_VERSION", required=True)

    return cfg


# ── Data loading ─────────────────────────────────────────────────────────────


def download_questions(data_dir: Path) -> Path:
    """Download question.jsonl from HuggingFace and return the local path."""
    data_dir.mkdir(parents=True, exist_ok=True)
    log.info(
        "Downloading %s from %s (%s)",
        HF_QUESTIONS_PATH,
        HF_REPO_ID,
        HF_REPO_TYPE,
    )
    local_path = hf_hub_download(
        repo_id=HF_REPO_ID,
        repo_type=HF_REPO_TYPE,
        filename=HF_QUESTIONS_PATH,
        local_dir=str(data_dir),
    )
    log.info("Downloaded questions to %s", local_path)
    return Path(local_path)


def load_questions(path: Path) -> list[dict]:
    questions = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                questions.append(json.loads(line))
    questions.sort(key=lambda q: q["question_id"])
    return questions


# ── Stage 1: Generate model answers ─────────────────────────────────────────


def generate_answers(
    questions: list[dict],
    kaito_endpoint: str,
    model_name: str,
    max_tokens: int,
    temperature: float,
) -> list[dict]:
    """Call the Kaito workspace for each MT-Bench question (2 turns each)."""
    client = OpenAI(
        base_url=f"{kaito_endpoint}/v1",
        api_key="unused",  # Kaito endpoints don't require auth
    )

    answers = []
    for q in questions:
        qid = q["question_id"]
        category = q["category"]
        turns = q["turns"]
        model_answers = []
        messages: list[dict] = []

        for turn_idx, turn_text in enumerate(turns):
            messages.append({"role": "user", "content": turn_text})
            try:
                resp = client.chat.completions.create(
                    model=model_name,
                    messages=messages,
                    max_completion_tokens=max_tokens,
                    temperature=temperature,
                )
                answer_text = resp.choices[0].message.content or ""
            except Exception as e:
                log.warning(
                    "Failed to get answer for q%d turn %d: %s", qid, turn_idx + 1, e
                )
                answer_text = f"[ERROR] {e}"

            model_answers.append(answer_text)
            messages.append({"role": "assistant", "content": answer_text})

        record = {
            "question_id": qid,
            "category": category,
            "model_id": model_name,
            "turns": turns,
            "answers": model_answers,
        }
        answers.append(record)
        log.info("Generated answers for q%d (%s)", qid, category)

    return answers


# ── Stage 2: Evaluate answers using judge LLM ────────────────────────────

# Judge prompt templates (single-answer grading)
SINGLE_GENERAL = "single-v1"
SINGLE_MATH = "single-math-v1"
SINGLE_GENERAL_MULTI = "single-v1-multi-turn"
SINGLE_MATH_MULTI = "single-math-v1-multi-turn"

MATH_CATEGORIES = {"math", "coding"}


def _select_judge_prompt(
    judge_prompts: dict[str, dict], category: str, turn: int
) -> dict:
    """Select the appropriate judge prompt based on category and turn."""
    if turn == 1:
        name = SINGLE_MATH if category in MATH_CATEGORIES else SINGLE_GENERAL
    else:
        name = (
            SINGLE_MATH_MULTI if category in MATH_CATEGORIES else SINGLE_GENERAL_MULTI
        )
    return judge_prompts[name]


def _build_judge_message(
    judge_prompt: dict,
    question: dict,
    answer_record: dict,
    turn: int,
) -> list[dict]:
    """Build the judge conversation for a single grading call."""
    system_prompt = judge_prompt["system_prompt"]
    template = judge_prompt["prompt_template"]

    turns = question["turns"]
    answers = answer_record["answers"]
    ref = question.get("reference", ["", ""])

    if turn == 1:
        user_content = template.format(
            question=turns[0],
            answer=answers[0],
            ref_answer_1=ref[0] if ref else "",
        )
    else:
        user_content = template.format(
            question_1=turns[0],
            answer_1=answers[0],
            question_2=turns[1] if len(turns) > 1 else "",
            answer_2=answers[1] if len(answers) > 1 else "",
            ref_answer_1=ref[0] if ref else "",
            ref_answer_2=ref[1] if len(ref) > 1 else "",
        )

    return [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": user_content},
    ]


def _parse_score(judgment_text: str) -> int:
    """Extract the [[rating]] from the judge response."""
    match = re.search(r"\[\[(\d+(?:\.\d+)?)\]\]", judgment_text)
    if match:
        return int(float(match.group(1)))
    log.warning("Could not parse score from judgment: %s", judgment_text[:200])
    return -1


def _judge_single(
    judge_client: Any,
    deployment: str,
    judge_prompts: dict[str, dict],
    question: dict,
    answer_record: dict,
    turn: int,
) -> dict:
    """Judge a single (question, answer, turn) and return the judgment record."""
    qid = question["question_id"]
    category = question["category"]
    judge_prompt = _select_judge_prompt(judge_prompts, category, turn)
    messages = _build_judge_message(judge_prompt, question, answer_record, turn)

    try:
        resp = judge_client.chat.completions.create(
            model=deployment,
            messages=messages,
            max_completion_tokens=2048,
            temperature=0,
        )
        judgment_text = resp.choices[0].message.content or ""
    except Exception as e:
        log.warning("Judge call failed for q%d turn %d: %s", qid, turn, e)
        judgment_text = f"[ERROR] {e}"

    score = _parse_score(judgment_text)

    return {
        "question_id": qid,
        "category": category,
        "turn": turn,
        "model_id": answer_record["model_id"],
        "judge_model": deployment,
        "score": score,
        "judgment": judgment_text,
    }


def judge_answers(
    questions: list[dict],
    answers: list[dict],
    judge_prompts: dict[str, dict],
    judge_source: str,
    judge_endpoint: str,
    judge_api_key: str,
    azure_deployment: str,
    azure_api_version: str,
    parallel: int,
) -> list[dict]:
    """Judge all answers using a strong model."""
    if judge_source == "MICROSOFT_AI_FOUNDRY":
        judge_client = AzureOpenAI(
            azure_endpoint=judge_endpoint,
            api_key=judge_api_key,
            api_version=azure_api_version,
        )
    else:
        raise ValueError(f"Unsupported JUDGE_SOURCE: {judge_source}")

    # Build index of answers by question_id
    answer_by_qid = {a["question_id"]: a for a in answers}

    # Prepare all judge tasks: (question, answer_record, turn)
    tasks = []
    for q in questions:
        qid = q["question_id"]
        if qid not in answer_by_qid:
            continue
        answer_rec = answer_by_qid[qid]
        tasks.append((q, answer_rec, 1))
        if len(q["turns"]) > 1:
            tasks.append((q, answer_rec, 2))

    # Execute judgments, optionally in parallel
    judgments = []
    log.info(
        "Judging %d (question, turn) pairs with parallelism=%d", len(tasks), parallel
    )

    def _do_judge(task):
        q, a, turn = task
        return _judge_single(judge_client, azure_deployment, judge_prompts, q, a, turn)

    with concurrent.futures.ThreadPoolExecutor(max_workers=parallel) as executor:
        futures = {executor.submit(_do_judge, t): t for t in tasks}
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            judgments.append(result)
            log.info(
                "Judged q%d turn %d: score=%d",
                result["question_id"],
                result["turn"],
                result["score"],
            )

    judgments.sort(key=lambda j: (j["question_id"], j["turn"]))
    return judgments


# ── Stage 3: Aggregate results ───────────────────────────────────────────────

CATEGORIES = [
    "writing",
    "roleplay",
    "reasoning",
    "math",
    "coding",
    "extraction",
    "stem",
    "humanities",
]


def aggregate_results(
    judgments: list[dict], model_name: str, judge_deployment: str
) -> dict:
    """Compute per-category and overall averages."""
    # Per-category, per-turn scores
    cat_turn_scores: dict[str, dict[int, list[int]]] = {}
    for j in judgments:
        if j["score"] < 0:
            continue  # Skip failed parses
        cat = j["category"]
        turn = j["turn"]
        cat_turn_scores.setdefault(cat, {}).setdefault(turn, []).append(j["score"])

    # Category averages (average of turn 1 avg and turn 2 avg)
    category_scores = {}
    all_turn1_scores = []
    all_turn2_scores = []
    for cat in CATEGORIES:
        turns = cat_turn_scores.get(cat, {})
        t1 = turns.get(1, [])
        t2 = turns.get(2, [])
        if t1:
            all_turn1_scores.extend(t1)
        if t2:
            all_turn2_scores.extend(t2)
        turn_avgs = []
        if t1:
            turn_avgs.append(sum(t1) / len(t1))
        if t2:
            turn_avgs.append(sum(t2) / len(t2))
        category_scores[cat] = (
            round(sum(turn_avgs) / len(turn_avgs), 2) if turn_avgs else 0.0
        )

    turn1_avg = (
        round(sum(all_turn1_scores) / len(all_turn1_scores), 2)
        if all_turn1_scores
        else 0.0
    )
    turn2_avg = (
        round(sum(all_turn2_scores) / len(all_turn2_scores), 2)
        if all_turn2_scores
        else 0.0
    )

    # Overall = average of per-category scores (matching MT-Bench methodology)
    valid_cats = [s for s in category_scores.values() if s > 0]
    overall = round(sum(valid_cats) / len(valid_cats), 2) if valid_cats else 0.0

    num_scored = sum(1 for j in judgments if j["score"] >= 0)

    return {
        "model_id": model_name,
        "overall_score": overall,
        "turn1_score": turn1_avg,
        "turn2_score": turn2_avg,
        "categories": category_scores,
        "num_questions": len(set(j["question_id"] for j in judgments)),
        "num_scored": num_scored,
        "judge_model": judge_deployment,
    }


# ── Output ───────────────────────────────────────────────────────────────────


def write_results(
    results_dir: Path,
    answers: list[dict],
    judgments: list[dict],
    report: dict,
) -> None:
    """Write detailed results to files."""
    results_dir.mkdir(parents=True, exist_ok=True)

    answers_path = results_dir / "mt_bench_answers.jsonl"
    with open(answers_path, "w") as f:
        for a in answers:
            f.write(json.dumps(a) + "\n")
    log.info("Wrote answers to %s", answers_path)

    judgments_path = results_dir / "mt_bench_judgments.jsonl"
    with open(judgments_path, "w") as f:
        for j in judgments:
            f.write(json.dumps(j) + "\n")
    log.info("Wrote judgments to %s", judgments_path)

    report_path = results_dir / "mt_bench_report.json"
    with open(report_path, "w") as f:
        json.dump(report, f, indent=2)
    log.info("Wrote report to %s", report_path)


def emit_result_line(report: dict) -> None:
    """Emit the structured result line to stdout for controller parsing."""
    payload = json.dumps(report, separators=(",", ":"))
    # Write to stdout so it appears in `kubectl logs`
    print(f"KAITO_MTBENCH_RESULT {payload}", flush=True)


def print_summary(report: dict) -> None:
    """Print a human-readable summary."""
    print("\n" + "=" * 60)
    print(f"MT-Bench Results for: {report['model_id']}")
    print(f"Judge: {report['judge_model']}")
    print("=" * 60)
    print(f"  Overall Score:  {report['overall_score']}")
    print(f"  Turn 1 Average: {report['turn1_score']}")
    print(f"  Turn 2 Average: {report['turn2_score']}")
    print(f"  Questions: {report['num_questions']} | Scored: {report['num_scored']}")
    print("-" * 60)
    print("  Category Scores:")
    for cat in CATEGORIES:
        score = report["categories"].get(cat, 0.0)
        bar = "#" * int(score)
        print(f"    {cat:<12s} {score:>5.2f}  {bar}")
    print("=" * 60 + "\n", flush=True)


# ── Main ─────────────────────────────────────────────────────────────────────


def main() -> None:
    cfg = load_config()

    questions_path = download_questions(DATA_DIR)
    log.info("Loading MT-Bench questions from %s", questions_path)
    questions = load_questions(questions_path)
    log.info("Loaded %d questions", len(questions))

    judge_prompts = JUDGE_PROMPTS
    log.info("Using %d hardcoded judge prompt templates", len(judge_prompts))

    # ── Stage 1: Generate answers ────────────────────────────────────────
    log.info(
        "Stage 1: Generating answers from %s (model=%s)",
        cfg["kaito_endpoint"],
        cfg["model_name"],
    )
    t0 = time.time()
    answers = generate_answers(
        questions,
        cfg["kaito_endpoint"],
        cfg["model_name"],
        cfg["max_tokens"],
        cfg["temperature"],
    )
    t1 = time.time()
    log.info("Stage 1 complete: %d answers in %.1fs", len(answers), t1 - t0)

    # ── Stage 2: Judge answers ───────────────────────────────────────────
    if cfg["dry_run"]:
        log.info("DRY_RUN=true — skipping judging stage")
        judgments = []
        report = {
            "model_id": cfg["model_name"],
            "overall_score": 0.0,
            "turn1_score": 0.0,
            "turn2_score": 0.0,
            "categories": {cat: 0.0 for cat in CATEGORIES},
            "num_questions": len(questions),
            "num_scored": 0,
            "judge_model": cfg["azure_deployment"],
            "dry_run": True,
        }
    else:
        log.info(
            "Stage 2: Judging with %s deployment=%s (parallel=%d)",
            cfg["judge_source"],
            cfg["azure_deployment"],
            cfg["parallel"],
        )
        t2 = time.time()
        judgments = judge_answers(
            questions,
            answers,
            judge_prompts,
            cfg["judge_source"],
            cfg["judge_endpoint"],
            cfg["judge_api_key"],
            cfg["azure_deployment"],
            cfg["azure_api_version"],
            cfg["parallel"],
        )
        t3 = time.time()
        log.info("Stage 2 complete: %d judgments in %.1fs", len(judgments), t3 - t2)

        # ── Stage 3: Aggregate ───────────────────────────────────────────
        report = aggregate_results(
            judgments, cfg["model_name"], cfg["azure_deployment"]
        )

    # ── Write output ─────────────────────────────────────────────────────
    write_results(cfg["results_dir"], answers, judgments, report)
    print_summary(report)
    emit_result_line(report)


if __name__ == "__main__":
    main()
