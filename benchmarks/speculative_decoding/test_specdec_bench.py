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

import pytest
import specdec_bench as sb


def test_parse_concurrency():
    assert sb.parse_concurrency("8, 1,8 ,2,") == [1, 2, 8]
    for bad in ["", "0", "1,-2", "a"]:
        with pytest.raises(sb.BenchError):
            sb.parse_concurrency(bad)


def test_parse_labels():
    assert sb.parse_labels(["kubernetes.io/hostname=node-1"]) == {
        "kubernetes.io/hostname": "node-1"
    }
    for bad in ["novalue", "bad key=x", "k=$(rm -rf /)"]:
        with pytest.raises(sb.BenchError):
            sb.parse_labels([bad])


def test_build_workspace_is_minimal():
    ws = sb.build_workspace(
        "bench", "org/model", "Standard_ND96isr_H100_v5", {"a": "b"}
    )
    assert ws["metadata"] == {"name": "bench", "annotations": {sb.ANNOTATION: "false"}}
    assert ws["resource"] == {
        "labelSelector": {"matchLabels": {"a": "b"}},
        "instanceType": "Standard_ND96isr_H100_v5",
    }
    assert ws["inference"] == {"preset": {"name": "org/model"}}
    assert "instanceType" not in sb.build_workspace("b", "p", None, {})["resource"]


def test_build_client_pod():
    server = {
        "image": "img:1",
        "node": "node-1",
        "tolerations": [{"operator": "Exists"}],
    }
    spec = sb.build_client_pod("client", server)["spec"]
    container = spec["containers"][0]
    assert container["image"] == "img:1"
    assert spec["nodeName"] == "node-1"
    assert spec["tolerations"] == [{"operator": "Exists"}]
    assert "resources" not in container
    assert container["env"] == [{"name": "NVIDIA_VISIBLE_DEVICES", "value": "void"}]


def test_bench_command_uses_vllm_defaults():
    cmd = sb.bench_command("http://ws.ns.svc.cluster.local:80", "mtbench", 8, "c8.json")
    assert cmd[:3] == ["vllm", "bench", "serve"]

    def flag(name):
        return cmd[cmd.index(name) + 1]

    assert flag("--dataset-path") == "philschmid/mt-bench"
    assert flag("--max-concurrency") == "8"
    assert flag("--num-prompts") == "32"
    assert flag("--result-filename") == "c8.json"
    # Left to vllm bench serve: model and tokenizer come from /v1/models.
    for absent in ["--model", "--tokenizer", "--temperature", "--seed"]:
        assert absent not in cmd


def test_bench_command_dataset():
    cmd = sb.bench_command("http://x", "instructcoder", 1, "c1.json")
    assert cmd[cmd.index("--dataset-path") + 1] == "likaixin/InstructCoder"


def test_num_prompts():
    assert sb.num_prompts(1) == 16
    assert sb.num_prompts(64) == 256


def test_speculative_config_of():
    line = 'inference_api.py --speculative-config=\'{"method":"dspark"}\' --dtype=auto'
    assert sb.speculative_config_of(line) == '{"method":"dspark"}'
    assert sb.speculative_config_of("--speculative-config={} --x") == "{}"
    assert sb.speculative_config_of("inference_api.py --dtype=auto") is None


def test_compare_and_report():
    def result(**overrides):
        data = {
            "output_throughput": 100.0,
            "mean_tpot_ms": 10.0,
            "median_ttft_ms": 150.0,
            "failed": 0,
        }
        return data | overrides

    results = {
        "baseline": {
            "mtbench": {1: result(), 64: result(failed=1), 128: result()},
            "instructcoder": {1: result()},
        },
        "speculative": {
            "mtbench": {
                1: result(
                    output_throughput=250.0,
                    mean_tpot_ms=4.0,
                    spec_decode_acceptance_length=2.9,
                ),
                64: result(output_throughput=90.0, mean_tpot_ms=11.0),
            },
            "instructcoder": {1: result(output_throughput=300.0)},
        },
    }
    rows = sb.compare(results)
    assert [(r["dataset"], r["concurrency"]) for r in rows] == [
        ("mtbench", 1),
        ("mtbench", 64),
        ("instructcoder", 1),
    ]
    assert rows[2]["throughput_gain"] == pytest.approx(3.0)
    assert rows[0]["throughput_gain"] == pytest.approx(2.5)
    assert rows[0]["decode_speedup"] == pytest.approx(2.5)
    assert rows[0]["acceptance_length"] == 2.9
    assert rows[1]["throughput_gain"] == pytest.approx(0.9)
    assert rows[1]["failed_requests"] == 1

    report = sb.render_report("org/model", '{"method":"mtp"}', rows)
    assert "`org/model`" in report
    assert '`{"method":"mtp"}`' in report
    assert (
        "| 1 | 100 → 250 | **2.50x** | 10.00 → 4.00 | **2.50x** | 150 → 150 | 2.90 |"
        in report
    )
    assert "| n/a |" in report
    assert "Some requests failed" in report
    assert "## mtbench (`philschmid/mt-bench`)" in report
    assert "## instructcoder (`likaixin/InstructCoder`)" in report
    assert report.index("## mtbench") < report.index("## instructcoder")


@pytest.mark.parametrize(
    "argv",
    [
        ["--preset", "p", "--workspace", "Bad_Name"],
        ["--preset", "p", "--concurrency", "0"],
        ["--preset", "p", "--label-selector", "novalue"],
        ["--preset", "p", "--datasets", "mtbench,unknown"],
    ],
)
def test_invalid_arguments_fail_before_touching_the_cluster(argv):
    assert sb.main(argv) == 1


def test_spec_method():
    assert sb.spec_method('{"method":"dspark","num_speculative_tokens":5}') == "dspark"
    for bad in [None, "", "not json", "[]", "{}"]:
        assert sb.spec_method(bad) == "unknown method"


def plot_rows():
    rows = []
    for dataset in ("mtbench", "instructcoder"):
        for concurrency, base, spec in [(1, 100.0, 250.0), (8, 600.0, 900.0)]:
            rows.append(
                {
                    "dataset": dataset,
                    "concurrency": concurrency,
                    "baseline_output_tok_s": base,
                    "speculative_output_tok_s": spec,
                    "throughput_gain": spec / base,
                    "baseline_mean_tpot_ms": 8.0,
                    "speculative_mean_tpot_ms": 3.5 if concurrency == 1 else None,
                }
            )
    return rows


def test_plot_writes_png(tmp_path):
    pytest.importorskip("matplotlib")
    path = tmp_path / "speedup.png"
    assert sb.plot("org/model", '{"method":"mtp"}', plot_rows(), path)
    assert path.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n"


def test_plot_without_matplotlib(tmp_path, monkeypatch):
    import builtins

    real_import = builtins.__import__

    def fake_import(name, *args, **kwargs):
        if name.startswith("matplotlib"):
            raise ImportError(name)
        return real_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", fake_import)
    path = tmp_path / "speedup.png"
    assert not sb.plot("org/model", None, plot_rows(), path)
    assert not path.exists()


def test_parse_datasets():
    assert sb.build_parser().parse_args(["--preset", "p"]).datasets == "mtbench"
    assert sb.parse_datasets("mtbench") == ["mtbench"]
    assert sb.parse_datasets(" instructcoder, mtbench,instructcoder,") == [
        "instructcoder",
        "mtbench",
    ]
    for bad in ["", " , ", "unknown", "mtbench,unknown"]:
        with pytest.raises(sb.BenchError):
            sb.parse_datasets(bad)
