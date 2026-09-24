# GuideLLM regression data

`config.yaml` defines named startup benchmark profiles and metric direction. `baselines.yaml` contains reviewed TPM, TTFT, and TPOT measurements keyed by `(model, instanceType, nodes, profile)`.

The preset regression runner does not start another load test. It reads the three metrics already stored in Workspace status, verifies that their benchmark configuration matches the selected profile, and applies the tolerances from `config.yaml`.

Changing a profile requires recollecting every affected baseline. Runtime metadata and raw benchmark output remain in workflow artifacts.
