# Kubernetes AI Toolchain Operator (Kaito)

![GitHub Release](https://img.shields.io/github/v/release/kaito-project/kaito)
[![Go Report Card](https://goreportcard.com/badge/github.com/kaito-project/kaito)](https://goreportcard.com/report/github.com/kaito-project/kaito)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/kaito-project/kaito)
[![codecov](https://codecov.io/gh/kaito-project/kaito/graph/badge.svg?token=XAQLLPB2AR)](https://codecov.io/gh/kaito-project/kaito)

| ![notification](docs/img/bell.svg) What is NEW!                                                                                                                                                                                                |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Coming soon:** Kaito v0.5.0. Retrieval-augmented generation (RAG) - RagEngine support with LlamaIndex orchestration and Faiss as the default vectorDB, learn about recent updates [here](https://github.com/kaito-project/kaito/issues/734)! |
| Latest Release: May 14th, 2025. Kaito v0.4.6.                                                                                                                                                                                                  |
| First Release: Nov 15th, 2023. Kaito v0.1.0.                                                                                                                                                                                                   |

Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster.
The target models are popular open-sourced large models such as [falcon](https://huggingface.co/tiiuae) and [phi-3](https://huggingface.co/docs/transformers/main/en/model_doc/phi3).
Kaito has the following key differentiations compared to most of the mainstream model deployment methodologies built on top of virtual machine infrastructures:

- Manage large model files using container images. An OpenAI-compatible server is provided to perform inference calls.
- Provide preset configurations to avoid adjusting workload parameters based on GPU hardware.
- Provide support for popular open-sourced inference runtimes: [vLLM](https://github.com/vllm-project/vllm) and [transformers](https://github.com/huggingface/transformers).
- Auto-provision GPU nodes based on model requirements.
- Host large model images in the public Microsoft Container Registry (MCR) if the license allows.

Using Kaito, the workflow of onboarding large AI inference models in Kubernetes is largely simplified.

👉 For more details and how to get started, please refer to [the documentation](https://kaito-project.github.io/kaito/docs/).

## Contributing

[Read more](docs/contributing/readme.md)
<!-- markdown-link-check-disable -->
This project welcomes contributions and suggestions. The contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit [CLAs for CNCF](https://github.com/cncf/cla?tab=readme-ov-file).

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the CLAs for CNCF, please electronically sign the CLA via
https://easycla.lfx.linuxfoundation.org. If you encounter issues, you can submit a ticket with the
Linux Foundation ID group through the [Linux Foundation Support website](https://jira.linuxfoundation.org/plugins/servlet/desk/portal/4/create/143).

## License

See [MIT License](LICENSE).

## Code of Conduct

KAITO has adopted the [Cloud Native Compute Foundation Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). For more information see the [KAITO Code of Conduct](CODE_OF_CONDUCT.md).

<!-- markdown-link-check-enable -->
## Contact

"Kaito devs" <kaito-dev@microsoft.com>

[Kaito Community Slack](https://join.slack.com/t/kaito-z6a6575/shared_invite/zt-2wm17rttz-t4E6_rMIuY03DwBHaJq1sg)
