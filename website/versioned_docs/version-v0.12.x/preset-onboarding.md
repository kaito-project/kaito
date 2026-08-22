---
title: Preset onboarding
---

This document describes how to add a new supported OSS model in KAITO. The process is designed to allow community users to initiate the request. KAITO maintainers will follow up and deal with managing the model images and guiding the code changes to set up the model preset configurations.

## Step 1: Make a proposal

This step is done by the requester. The requester should make a PR to describe the target OSS model following this [template](https://github.com/kaito-project/kaito/blob/main/docs/proposals/YYYYMMDD-model-template.md). The proposal status should be `provisional` in the beginning. KAITO maintainers will review the PR and decide to accept or reject the PR. The PR could be rejected if the target OSS model has low usage, or it has strict license limitations, or it is a relatively small model with limited capabilities.


## Step 2: Onboard model to KAITO's model catalog

This step is done by KAITO maintainers. KAITO maintainers will onboard the targeted model following `presets/workspace/models/model_catalog.md`.
