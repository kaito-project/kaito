---
title: Custom Model Integration
---

## Option 1: Use Pre-Built Docker Image Without Model Weights
If you want to avoid building a Docker image with model weights, use our pre-built reference image (`ghcr.io/kaito-project/kaito/llm-reference-preset:latest`). This image, built with [Dockerfile.reference](../../examples/custom-model-integration/Dockerfile.reference), dynamically downloads model weights from HuggingFace at runtime, reducing the need to create and maintain custom images.


- **[Sample Deployment YAML](../../examples/custom-model-integration/reference-image-deployment.yaml)**


## Option 2: Build a Custom Docker Image with Model Weights

### Step 1: Clone the Repository

```sh
git clone https://github.com/kaito-project/kaito.git
```

### Step 2: Download Your Private/Custom Model Weights

For example, assuming HuggingFace weights:
```sh
git lfs install
git clone git@hf.co:<MODEL_ID>  # Example: git clone git@hf.co:bigscience/bloom
# OR
git clone https://huggingface.co/bigscience/bloom
```

Alternatively, use curl:
```
curl -sSL https://huggingface.co/bigscience/bloom/resolve/main/config.json?download=true -o config.json
```

More information on downloading models from HuggingFace can be found [here](https://huggingface.co/docs/hub/en/models-downloading).


### Step 3: Log In to Your Container Registry

Before pushing the Docker image, ensure you’re logged into the appropriate container registry. Here are general login methods depending on the registry you use:

1. GitHub Container Registry (ghcr.io):
```sh
echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin
```
Replace CR_PAT with your GitHub Personal Access Token and USERNAME with your GitHub username. This token should have the write:packages and read:packages permissions.

2. Azure Container Registry (ACR): If you're using ACR:

```sh
az acr login --name <REGISTRY_NAME>
```
Replace `<REGISTRY_NAME>` with your Azure Container Registry name.

3. Docker Hub or Other Container Registries:
```sh
docker login <REGISTRY_URL>
```
Enter your username and password when prompted. Replace `<REGISTRY_URL>` with your registry URL, such as `docker.io` for Docker Hub.


### Step 4: Build Docker Image with Private/Custom Weights

1. Set Environment Variables

Before building the Docker image, set the relevant environment variables for the image name, version, and weights path:
```sh
export IMAGE_NAME="modelsregistry.azurecr.io/phi-3-mini-4k-instruct:0.0.1"
export VERSION="0.0.1"
export WEIGHTS_PATH="kaito/phi-3-mini-4k-instruct/weights"
```

2. Build and Push the Docker Image

Navigate to the KAITO base directory and build the Docker image, ensuring the weights directory is included in the build context:
```sh
docker build -t <IMAGE_NAME> --file docker/presets/workspace/models/tfs/Dockerfile --build-arg WEIGHTS_PATH=<WEIGHTS_PATH> --build-arg MODEL_TYPE=text-generation --build-arg VERSION=<VERSION> .

docker push <IMAGE_NAME>
```

### Step 5: Deploy
Follow the [Custom Template](../../examples/custom-model-integration/custom-deployment-template.yaml)