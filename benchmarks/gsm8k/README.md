# GSM8K regression data

`config.yaml` defines the pinned GSM8K task and named generation profiles. `baselines.yaml` contains reviewed measurements keyed by `(model, instanceType, nodes, profile)`.

The preset regression runner resolves a model override or the default profile, evaluates the first 128 test examples, rejects empty final responses, and compares flexible exact-match accuracy with the target baseline. Baseline collection must use the same profile and deployment target. Raw evaluator output remains in workflow artifacts.

Changing the dataset, evaluator, sample selection, default profile, or a profile definition requires recollecting every affected baseline. Execution limits and comparison tolerances live in `config.yaml`.
