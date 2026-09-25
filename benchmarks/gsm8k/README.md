# GSM8K regression data

`config.yaml` defines the pinned GSM8K task and named generation profiles. `baselines.yaml` contains reviewed measurements keyed by `(model, instanceType, nodes, profile)`.

The preset regression runner resolves a model override or the default profile, evaluates the first 128 test examples, and compares flexible exact-match accuracy with the target baseline. Empty final responses count as incorrect samples and are recorded in each baseline's `emptyResponses` field. Baseline collection must use the same profile and deployment target. Raw evaluator output remains in workflow artifacts.

Changing the dataset, evaluator, sample selection, default profile, or a profile definition requires recollecting every affected baseline. Execution limits and comparison tolerances live in `config.yaml`.

Most reasoning models use `chat_template_kwargs.enable_thinking`. Mistral-format
tokenizers reject request-level chat-template kwargs, so Mistral reasoning models
use the native OpenAI `reasoning_effort: high` field through `requestKwargs`.
