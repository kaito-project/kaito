---
title: Proposals
---

This section contains proposals for adding new models to Kaito. Each proposal describes the process of evaluating and integrating new OSS models into the Kaito ecosystem.

## Proposal Template

Before creating a new model proposal, please use the following template: [Model Proposal Template](./proposals/YYYYMMDD-model-template)

## Current Proposals

Below are the current model proposals in various stages of integration:

### Provisional Status
- [Llama 3.3 70B Instruct](./proposals/20250529-llama-3.3-70b-instruct.md) - Meta's multilingual instruction-tuned 70B model
- [Qwen2.5 Coder](./proposals/20250103-qwen2.5-coder.md) - Qwen2.5 series for code generation
- [Phi-4 Instruct](./proposals/20241212-phi4-instruct.md) - Microsoft's latest Phi-4 instruction-tuned model
- [Distributed Inference](./proposals/20250325-distributed-inference.md) - Support for distributed inference across multiple GPUs
- [Model as OCI Artifacts](./proposals/20250609-model-as-oci-artifacts.md) - Packaging models as OCI artifacts

### Integrated Status
- [Mistral Instruct](./proposals/20240205-mistral-instruct.md) - Mistral AI's instruction-tuned model
- [Mistral](./proposals/20240205-mistral.md) - Base Mistral model
- [Phi-2](./proposals/20240206-phi-2.md) - Microsoft's Phi-2 small language model
- [Phi-3 Instruct](./proposals/20240527-phi3-instruct.md) - Microsoft's Phi-3 instruction-tuned model

## Proposal Process

For detailed information about the model onboarding process, see the [Model Onboarding Guide](./preset-onboarding.md).

### Step 1: Create a Proposal
Use the model proposal template to describe the target OSS model, including licensing, usage statistics, and technical requirements.

### Step 2: Model Validation
Kaito maintainers validate and test the proposed model using the specified runtime.

### Step 3: Image Publishing
If licensing allows, model images are published to Microsoft Container Registry (MCR).

### Step 4: Integration
Implement preset configurations and inference interfaces for the model.

### Step 5: Testing
Add comprehensive E2E tests to ensure the model works correctly with Kaito.

## Contributing a Proposal

To contribute a new model proposal:

1. Fork the Kaito repository
2. Copy the [model template](./proposals/YYYYMMDD-model-template) to `website/docs/proposals/YYYYMMDD-<model-name>.md`
3. Fill out all required sections
4. Submit a pull request for review

The proposal status will be updated as it progresses through the integration pipeline.
