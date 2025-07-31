---
title: Preset onboarding
---

This document describes how to add a new supported OSS model in KAITO. The process is designed to allow community users to initiate the request. KAITO maintainers will follow up and deal with managing the model images and guiding the code changes to set up the model preset configurations.

## Step 1: Make a proposal

This step is done by the requestor. The requestor should make a PR to describe the target OSS model following this [template](https://github.com/kaito-project/kaito/blob/main/docs/proposals/YYYYMMDD-model-template.md). The proposal status should be `provisional` in the beginning. KAITO maintainers will review the PR and decide to accept or reject the PR. The PR could be rejected if the target OSS model has low usage, or it has strict license limitations, or it is a relatively small model with limited capabilities.


## Step 2: Validate and test the model

This step is done by KAITO maintainers. Based on the information provided in the proposal, KAITO maintainers will download the model and test it using the specified runtime. The entire process is automated via GitHub actions when KAITO maintainers file a PR to add the model to the [supported\_models.yaml](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/supported_models.yaml).


## Step 3: Push model image to MCR

This step is done by KAITO maintainers. If the model license allows, KAITO maintainers will push the model image to MCR, making the image publicly available. This step is skipped if only private access is allowed for the model image. Once this step is done, KAITO maintainers will update the status of the proposal submitted in Step 1 to `ready to integrate`.

## Step 4: Add preset configurations

This step is done by the requestor. The requestor will work on a PR to register the model with preset configurations. The PR will contain code changes to implement a simple inference interface. [Here](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/falcon/model.go) is an existing example. In the same PR, or a separate PR, the status of the proposal status should be updated to `integrated`.

## Step 5: Add an E2E test

This step is done by the requestor. A new e2e test should be added to [here](https://github.com/kaito-project/kaito/blob/main/test/e2e/preset_test.go) which ensures the inference service is up and running with preset configurations.


After all the above are done, a new model becomes available in KAITO.
