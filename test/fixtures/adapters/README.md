# E2E Adapter Test Files

## Overview

These files are part of a set used for conducting end-to-end (E2E) testing of an adapter component. The Dockerfile builds an image incorporating the configuration and model files, which is then used within an Init Container for testing. The adapter1 and adapter2 are Phi-3-compatible LoRA adapters (copied from adapter-phi-3-mini-pycoder) used for testing adapter swap functionality with phi-3-mini-128k-instruct. The adapter-phi-3-mini-pycoder is a LoRA adapter trained for the Phi-3 Mini model family.

## Files

- **Dockerfile**: Builds the Docker image for the E2E tests.

- **adapter_config.json**: Contains settings for configuring the adapter in the test environment.

- **adapter_model.safetensors**: Provides the adapter's machine learning model in SafeTensors format.

## Usage

Build the Docker image with the following command:

```bash

make docker-build-adapter
 