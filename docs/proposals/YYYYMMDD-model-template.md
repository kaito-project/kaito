---
title: Proposal for new model support
authors:
  - "KAITO contributor"
reviewers:
  - "KAITO contributor"
creation-date: yyyy-mm-dd
last-updated: yyyy-mm-dd
status: provisional|ready to integrate|integrated
---

# Title
- Keep it simple and descriptive. E.g., Add XXXX (model name) to KAITO supported model list.

<!-- BEGIN Remove before PR -->
To get started with this template:
1. **Make a copy of this template.**
  Copy this template into `docs/proposals` and name it `YYYYMMDD-<model name>.md`, where `YYYYMMDD` is the date the proposal was first drafted.
1. **Fill out the required sections.**
1. **Create a PR.**


The `Metadata` section above is intended to support the creation of tooling around the proposal process.
This will be a YAML section that is fenced as a code block.

Note: if the intention is to add a model family that includes multiple models with different parameter sizes to KAITO, the PR author needs to create individual PR for **EACH** model, i.e., one proposal for one model specification.

<!-- END Remove before PR -->

## Glossary

If this proposal uses terms that need clarifications, define and describe them here.

## Summary

The `Summary` section is important for justifying the need of adding the proposed inference model in KAITO. This section needs to provide the following information.
- **Model description**: *What does the model do? Where are the official docs if any?*
- **Model usage statistics**: *What is the current download count? (source: e.g., huggingface or model website), or any statistics that indicate the model popularity, e.g., huggingface trending.*
- **Model license**: Note that for models with Apache 2 or MIT licenses, if the proposal is approved, the model images can be built by KAITO maintainers and hosted in public MCR. Otherwise, the KAITO users need to build the model images themselves in their private repositories.

<!-- BEGIN Remove before PR -->
There is always a cost of maintaining preset configurations and model images in KAITO. Hence, we prioritize supporting models with high popularities or emerging community interests first.
<!-- END Remove before PR -->

## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field | Notes|
|----|----|
| Family name| E.g., falcon, llama.|
| Type| huggingface classifications, e.g., `text-to-image` or `conversational` or `text generation`. |
| Download site| The link to the site that provides instructions about how to download the model files. |
| Version| A signature that represents the model version. It can be a commit hash, or a branch name based on the version control mechanism used in the download site. |
| Storage size| The required disk size to contain all model files. |
| GPU count| The minimum required GPU count to run the model. |
| Total GPU memory| The minimum required aggregated GPU memory to run the model. |
| Per GPU memory | The minimum required GPU memory per GPU. If not applicable, enter `N/A`. The mainstream GPU has 16-80GB memory. |


## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options | Notes|
|----|----|
| Runtime | E.g., huggingface transformer, or onnx. KAITO can support multiple runtimes (details TBD). |
| Distributed Inference| True/False. This indicates whether torch elastic should be configured or not. |
| Custom configurations| Describe custom configurations that will be used in the model deployment as defaults. For example, see [here](https://huggingface.co/docs/accelerate/basic_tutorials/launch#custom-configurations) for customizing the huggingface accelerate library.|

# History

- [ ] MM/DD/YYYY: Open proposal PR.
- [ ] MM/DD/YYYY: Start model integration.
- [ ] MM/DD/YYYY: Complete model support.
