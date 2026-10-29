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
"""Measure what KAITO's preset-driven speculative decoding buys under load.

Deploys a Workspace for a preset, benchmarks it with the
kaito.sh/enable-speculative-decoding annotation set to "false" and then "true",
and reports the output throughput gain and decode (time per output token)
speedup at each concurrency level. Load comes from `vllm bench serve` replaying
one or more datasets (MT-Bench by default) from a client pod in the cluster.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import time
from collections.abc import Sequence
from datetime import UTC, datetime
from pathlib import Path

ANNOTATION = "kaito.sh/enable-speculative-decoding"
VARIANTS = ("baseline", "speculative")
DNS1123_LABEL = re.compile(r"^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$")
LABEL_KEY = re.compile(
    r"^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?"
    r"[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$"
)
LABEL_VALUE = re.compile(r"^([A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?)?$")
SPEC_CONFIG_FLAG = re.compile(r"--speculative-config=(?:'([^']*)'|(\S+))")
# Hugging Face datasets `vllm bench serve` loads natively. Acceptance, and so the
# benefit, depends on the traffic: code editing repeats much of its prompt and
# is usually more draft-friendly than open-ended chat.
DATASETS = {
    "mtbench": "philschmid/mt-bench",  # multi-turn chat across 8 categories
    "instructcoder": "likaixin/InstructCoder",  # code editing
}


class BenchError(RuntimeError):
    """A failure that ends the run with a readable message."""


def log(message: str) -> None:
    print(f"[{datetime.now(UTC):%H:%M:%S}] {message}", file=sys.stderr, flush=True)


def parse_datasets(text: str) -> list[str]:
    """Parse a comma-separated dataset list, keeping order and dropping repeats."""
    datasets = list(dict.fromkeys(filter(None, (p.strip() for p in text.split(",")))))
    unknown = [d for d in datasets if d not in DATASETS]
    if unknown:
        raise BenchError(
            f"unknown dataset {', '.join(unknown)}; choose from {', '.join(DATASETS)}"
        )
    if not datasets:
        raise BenchError("at least one dataset is required")
    return datasets


def parse_concurrency(text: str) -> list[int]:
    levels = set()
    for part in filter(None, (p.strip() for p in text.split(","))):
        if not part.isdigit() or int(part) < 1:
            raise BenchError(f"invalid concurrency level {part!r}")
        levels.add(int(part))
    if not levels:
        raise BenchError("at least one concurrency level is required")
    return sorted(levels)


def parse_labels(pairs: Sequence[str]) -> dict[str, str]:
    labels = {}
    for pair in pairs:
        key, sep, value = pair.partition("=")
        if not sep or not LABEL_KEY.match(key) or not LABEL_VALUE.match(value):
            raise BenchError(f"invalid label {pair!r}; expected key=value")
        labels[key] = value
    return labels


def num_prompts(concurrency: int) -> int:
    """Enough requests to keep every slot busy for several rounds."""
    return max(16, 4 * concurrency)


def build_workspace(
    name: str, preset: str, instance_type: str | None, labels: dict[str, str]
) -> dict:
    resource: dict = {"labelSelector": {"matchLabels": labels}}
    if instance_type:
        resource["instanceType"] = instance_type
    return {
        "apiVersion": "kaito.sh/v1beta1",
        "kind": "Workspace",
        "metadata": {"name": name, "annotations": {ANNOTATION: "false"}},
        "resource": resource,
        "inference": {"preset": {"name": preset}},
    }


def build_client_pod(name: str, server: dict) -> dict:
    # Same image as the server so the client's vLLM matches and the image is
    # already on the node; the client only sends HTTP load, so no GPU.
    return {
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {"name": name},
        "spec": {
            "restartPolicy": "Never",
            "nodeName": server["node"],
            "tolerations": server["tolerations"],
            "containers": [
                {
                    "name": "client",
                    "image": server["image"],
                    "command": ["sleep", "infinity"],
                    "env": [{"name": "NVIDIA_VISIBLE_DEVICES", "value": "void"}],
                }
            ],
        },
    }


def bench_command(
    base_url: str, dataset: str, concurrency: int, result_file: str
) -> list[str]:
    # The model, tokenizer, output length and sampling are vllm bench serve's
    # defaults: the model and tokenizer come from the server's /v1/models.
    # Prompts are sent to the chat endpoint unformatted so the server applies
    # the model's own chat template.
    return [
        "vllm",
        "bench",
        "serve",
        "--base-url",
        base_url,
        "--backend",
        "openai-chat",
        "--endpoint",
        "/v1/chat/completions",
        "--dataset-name",
        "hf",
        "--dataset-path",
        DATASETS[dataset],
        "--skip-chat-template",
        "--max-concurrency",
        str(concurrency),
        "--num-prompts",
        str(num_prompts(concurrency)),
        "--save-result",
        "--result-dir",
        "/tmp",
        "--result-filename",
        result_file,
    ]


def speculative_config_of(command_line: str) -> str | None:
    match = SPEC_CONFIG_FLAG.search(command_line)
    if not match:
        return None
    return match.group(1) if match.group(1) is not None else match.group(2)


def _ratio(numerator: float | None, denominator: float | None) -> float | None:
    return numerator / denominator if numerator and denominator else None


def compare(results: dict[str, dict[str, dict[int, dict]]]) -> list[dict]:
    """Pair results[variant][dataset][concurrency] into one row per dataset and level."""
    rows = []
    for dataset, baseline in results["baseline"].items():
        for concurrency in sorted(baseline):
            base = baseline[concurrency]
            spec = results["speculative"].get(dataset, {}).get(concurrency)
            if spec:
                rows.append(_compare_level(dataset, concurrency, base, spec))
    return rows


def _compare_level(dataset: str, concurrency: int, base: dict, spec: dict) -> dict:
    return {
        "dataset": dataset,
        "concurrency": concurrency,
        "baseline_output_tok_s": base.get("output_throughput"),
        "speculative_output_tok_s": spec.get("output_throughput"),
        "throughput_gain": _ratio(
            spec.get("output_throughput"), base.get("output_throughput")
        ),
        "baseline_mean_tpot_ms": base.get("mean_tpot_ms"),
        "speculative_mean_tpot_ms": spec.get("mean_tpot_ms"),
        "decode_speedup": _ratio(base.get("mean_tpot_ms"), spec.get("mean_tpot_ms")),
        "baseline_median_ttft_ms": base.get("median_ttft_ms"),
        "speculative_median_ttft_ms": spec.get("median_ttft_ms"),
        "acceptance_length": spec.get("spec_decode_acceptance_length"),
        "failed_requests": (base.get("failed") or 0) + (spec.get("failed") or 0),
    }


def _fmt(value: float | None, pattern: str) -> str:
    return "n/a" if value is None else format(value, pattern)


def render_report(preset: str, spec_config: str | None, rows: list[dict]) -> str:
    lines = [
        f"# Speculative decoding benefit under load: `{preset}`",
        "",
        f"Injected `--speculative-config`: `{spec_config}`",
        "",
        "Throughput gain is speculative ÷ baseline output tokens per second. "
        "Decode speedup is baseline ÷ speculative mean time per output token, "
        "i.e. how much faster each user sees tokens stream.",
    ]
    for dataset in dict.fromkeys(r["dataset"] for r in rows):
        lines += _dataset_table(dataset, [r for r in rows if r["dataset"] == dataset])
    if any(r["failed_requests"] for r in rows):
        lines += ["", "> Some requests failed; check the per-level logs."]
    return "\n".join(lines) + "\n"


def _dataset_table(dataset: str, rows: list[dict]) -> list[str]:
    lines = [
        "",
        f"## {dataset} (`{DATASETS.get(dataset, dataset)}`)",
        "",
        "| Concurrency | Output tok/s (base → spec) | Throughput gain "
        "| Mean TPOT ms (base → spec) | Decode speedup "
        "| Median TTFT ms (base → spec) | Acceptance length |",
        "|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for r in rows:
        lines.append(
            f"| {r['concurrency']} "
            f"| {_fmt(r['baseline_output_tok_s'], '.0f')} → "
            f"{_fmt(r['speculative_output_tok_s'], '.0f')} "
            f"| **{_fmt(r['throughput_gain'], '.2f')}x** "
            f"| {_fmt(r['baseline_mean_tpot_ms'], '.2f')} → "
            f"{_fmt(r['speculative_mean_tpot_ms'], '.2f')} "
            f"| **{_fmt(r['decode_speedup'], '.2f')}x** "
            f"| {_fmt(r['baseline_median_ttft_ms'], '.0f')} → "
            f"{_fmt(r['speculative_median_ttft_ms'], '.0f')} "
            f"| {_fmt(r['acceptance_length'], '.2f')} |"
        )
    return lines


def spec_method(spec_config: str | None) -> str:
    try:
        return json.loads(spec_config or "")["method"]
    except (ValueError, KeyError, TypeError):
        return "unknown method"


def plot(preset: str, spec_config: str | None, rows: list[dict], path: Path) -> bool:
    """Draw throughput, per-user speed and their trade-off per dataset.

    Returns False, without plotting, when matplotlib is not installed.
    """
    try:
        import matplotlib

        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        log("matplotlib is not installed; skipping the plot")
        return False

    datasets = list(dict.fromkeys(r["dataset"] for r in rows))
    fig, axes = plt.subplots(
        len(datasets), 3, figsize=(17, 4.8 * len(datasets)), squeeze=False
    )
    for row_axes, dataset in zip(axes, datasets, strict=True):
        _plot_dataset(row_axes, dataset, [r for r in rows if r["dataset"] == dataset])
    fig.suptitle(
        f"{preset}: baseline vs speculative decoding ({spec_method(spec_config)})"
    )
    fig.tight_layout()
    fig.savefig(path, dpi=120)
    plt.close(fig)
    return True


def _plot_dataset(axes, dataset: str, rows: list[dict]) -> None:
    nan = float("nan")

    def per_user(tpot_ms: float | None) -> float:
        return 1000 / tpot_ms if tpot_ms else nan

    tput, speed, tradeoff = axes
    levels = [r["concurrency"] for r in rows]
    for variant, style in (("baseline", "o-"), ("speculative", "s-")):
        throughput = [r[f"{variant}_output_tok_s"] or nan for r in rows]
        user_speed = [per_user(r[f"{variant}_mean_tpot_ms"]) for r in rows]
        tput.plot(levels, throughput, style, label=variant)
        speed.plot(levels, user_speed, style, label=variant)
        tradeoff.plot(throughput, user_speed, style, label=variant)
        for level, x, y in zip(levels, throughput, user_speed, strict=True):
            if x == x and y == y:  # skip NaN gaps
                tradeoff.annotate(
                    str(level),
                    (x, y),
                    xytext=(3, 3),
                    textcoords="offset points",
                    fontsize=7,
                )
    gain = tput.twinx()
    gains = [r["throughput_gain"] or nan for r in rows]
    gain.plot(levels, gains, "k--", alpha=0.5, label="throughput gain")
    gain.set_ylabel("throughput gain (speculative ÷ baseline)")
    tput.set(
        xscale="log",
        xlabel="concurrency",
        ylabel="output tok/s",
        title=f"{dataset}: aggregate throughput",
    )
    speed.set(
        xscale="log",
        xlabel="concurrency",
        ylabel="tok/s per user (1000 / TPOT)",
        title=f"{dataset}: per-user decode speed",
    )
    tradeoff.set(
        xlabel="aggregate output tok/s",
        ylabel="tok/s per user",
        title=f"{dataset}: throughput vs interactivity",
    )
    for ax in axes:
        ax.grid(alpha=0.3)
    # Areas the curves leave empty: throughput rises to the right while the gain
    # and per-user speed fall.
    handles = tput.get_legend_handles_labels()
    gain_handles = gain.get_legend_handles_labels()
    tput.legend(
        handles[0] + gain_handles[0], handles[1] + gain_handles[1], loc="upper center"
    )
    speed.legend(loc="upper right")
    tradeoff.legend(loc="upper right")


class Kube:
    """kubectl wrapper; arguments are passed as a list, never through a shell."""

    def __init__(self, context: str | None, namespace: str):
        self.base = ["kubectl", *(["--context", context] if context else [])]
        self.namespace = namespace

    def run(
        self, args: Sequence[str], *, stdin: str | None = None, check: bool = True
    ) -> subprocess.CompletedProcess:
        cmd = [*self.base, "-n", self.namespace, *args]
        result = subprocess.run(cmd, input=stdin, capture_output=True, text=True)
        if check and result.returncode != 0:
            detail = (result.stderr or result.stdout).strip()
            raise BenchError(f"{' '.join(cmd)} failed: {detail}")
        return result

    def get(self, *args: str) -> dict | None:
        result = self.run(["get", *args, "-o", "json"], check=False)
        return json.loads(result.stdout) if result.returncode == 0 else None

    def apply(self, obj: dict) -> None:
        self.run(["apply", "-f", "-"], stdin=json.dumps(obj))


def _ready(obj: dict, condition: str) -> bool:
    conditions = (obj.get("status") or {}).get("conditions") or []
    return any(c["type"] == condition and c["status"] == "True" for c in conditions)


def wait_for_variant(
    kube: Kube, workspace: str, speculative: bool, timeout: float
) -> dict:
    """Wait until every serving pod is Ready and runs the requested variant."""
    log(f"waiting for speculative decoding {'on' if speculative else 'off'}")
    serving_pod = re.compile(rf"^{re.escape(workspace)}-\d+$")
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        pods = [
            p
            for p in (
                kube.get("pods", "-l", f"kaito.sh/workspace={workspace}") or {}
            ).get("items", [])
            if serving_pod.match(p["metadata"]["name"])
        ]
        matching = []
        for pod in pods:
            containers = pod["spec"]["containers"]
            container = next(
                (c for c in containers if c["name"] == workspace), containers[0]
            )
            config = speculative_config_of(
                " ".join(container.get("command", []) + container.get("args", []))
            )
            if (
                _ready(pod, "Ready")
                and not pod["metadata"].get("deletionTimestamp")
                and (config is not None) == speculative
            ):
                matching.append((pod, container, config))
        ws = kube.get("workspace", workspace) or {}
        if pods and len(matching) == len(pods) and _ready(ws, "InferenceReady"):
            pod, container, config = matching[0]
            return {
                "speculative_config": config,
                "image": container["image"],
                "node": pod["spec"]["nodeName"],
                "tolerations": pod["spec"].get("tolerations", []),
            }
        time.sleep(20)
    raise BenchError(f"timed out after {timeout:.0f}s waiting for {workspace}")


def run_benchmark(
    kube: Kube,
    client: str,
    base_url: str,
    dataset: str,
    concurrency: int,
    log_file: Path,
) -> dict:
    result_file = f"{dataset}-c{concurrency}.json"
    cmd = bench_command(base_url, dataset, concurrency, result_file)
    run = kube.run(["exec", client, "--", *cmd], check=False)
    log_file.parent.mkdir(parents=True, exist_ok=True)
    log_file.write_text(run.stdout + run.stderr)
    if run.returncode != 0:
        raise BenchError(f"vllm bench serve failed; see {log_file}")
    return json.loads(
        kube.run(["exec", client, "--", "cat", f"/tmp/{result_file}"]).stdout
    )


def benchmark_variant(
    kube: Kube,
    workspace: str,
    server: dict,
    datasets: list[str],
    levels: list[int],
    out_dir: Path,
) -> dict[str, dict[int, dict]]:
    client = f"{workspace}-bench-client"
    kube.run(["delete", "pod", client, "--ignore-not-found", "--wait=true"])
    kube.apply(build_client_pod(client, server))
    kube.run(["wait", "--for=condition=Ready", f"pod/{client}", "--timeout=900s"])
    base_url = f"http://{workspace}.{kube.namespace}.svc.cluster.local:80"

    # Unmeasured run at the highest level: every engine replica (e.g. each
    # data-parallel rank) pays its one-time compilation costs here instead of
    # inside the low-concurrency results.
    log("warm-up")
    run_benchmark(
        kube, client, base_url, datasets[0], levels[-1], out_dir / "warmup.log"
    )

    results: dict[str, dict[int, dict]] = {}
    for dataset in datasets:
        results[dataset] = {}
        for concurrency in levels:
            name = out_dir / dataset / f"c{concurrency}"
            result = run_benchmark(
                kube, client, base_url, dataset, concurrency, name.with_suffix(".log")
            )
            name.with_suffix(".json").write_text(json.dumps(result, indent=2))
            log(
                f"{dataset} concurrency {concurrency}: "
                f"{result['output_throughput']:.0f} output tok/s, "
                f"mean TPOT {result['mean_tpot_ms']:.2f} ms"
            )
            results[dataset][concurrency] = result
    return results


def run(args: argparse.Namespace) -> None:
    for value, what in ((args.namespace, "namespace"), (args.workspace, "workspace")):
        if not DNS1123_LABEL.match(value):
            raise BenchError(f"{what} {value!r} is not a valid Kubernetes name")
    levels = parse_concurrency(args.concurrency)
    datasets = parse_datasets(args.datasets)
    labels = parse_labels(args.label_selector) or {"apps": args.workspace}
    kube = Kube(args.context, args.namespace)
    out_dir = Path(args.output_dir)

    if kube.get("workspace", args.workspace):
        raise BenchError(f"workspace {args.workspace} already exists")
    if kube.run(["get", "namespace", args.namespace], check=False).returncode != 0:
        kube.run(["create", "namespace", args.namespace])
    kube.apply(build_workspace(args.workspace, args.preset, args.instance_type, labels))
    try:
        results, spec_config = {}, None
        for variant in VARIANTS:
            speculative = variant == "speculative"
            kube.run(
                ["annotate", "workspace", args.workspace, "--overwrite"]
                + [f"{ANNOTATION}={str(speculative).lower()}"]
            )
            server = wait_for_variant(kube, args.workspace, speculative, args.timeout)
            if speculative:
                spec_config = server["speculative_config"]
                log(f"injected --speculative-config: {spec_config}")
            results[variant] = benchmark_variant(
                kube, args.workspace, server, datasets, levels, out_dir / variant
            )
        rows = compare(results)
        report = render_report(args.preset, spec_config, rows)
        if plot(args.preset, spec_config, rows, out_dir / "speedup.png"):
            report += "\n![speedup](speedup.png)\n"
        (out_dir / "summary.json").write_text(json.dumps(rows, indent=2) + "\n")
        (out_dir / "REPORT.md").write_text(report)
        print(report)
    finally:
        log("cleaning up")
        kube.run(
            ["delete", "pod", f"{args.workspace}-bench-client", "--ignore-not-found"],
            check=False,
        )
        kube.run(
            ["delete", "workspace", args.workspace, "--ignore-not-found"], check=False
        )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preset", required=True, help="KAITO preset name")
    parser.add_argument("--instance-type", help="GPU SKU for resource.instanceType")
    parser.add_argument(
        "--label-selector",
        action="append",
        default=[],
        metavar="KEY=VALUE",
        help="resource.labelSelector entry, repeatable (default: apps=<workspace>)",
    )
    parser.add_argument(
        "--concurrency",
        default="1,2,4,8,16,32,64",
        help="comma-separated concurrency levels (default: %(default)s)",
    )
    parser.add_argument(
        "--datasets",
        default="mtbench",
        help="comma-separated datasets to replay, from: "
        f"{', '.join(DATASETS)} (default: %(default)s)",
    )
    parser.add_argument("--context", help="kubeconfig context (default: current)")
    parser.add_argument(
        "--namespace", default="specdec-bench", help="(default: %(default)s)"
    )
    parser.add_argument(
        "--workspace", default="specdec-bench", help="(default: %(default)s)"
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=3 * 3600,
        help="seconds to wait for each variant to become ready (default: %(default)s)",
    )
    parser.add_argument(
        "--output-dir", default="specdec-results", help="(default: %(default)s)"
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        run(args)
    except BenchError as err:
        log(f"error: {err}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
