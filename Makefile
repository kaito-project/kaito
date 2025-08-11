
# Image URL to use all building/pushing image targets
REGISTRY ?= YOUR_REGISTRY
IMG_NAME ?= workspace
VERSION ?= v0.6.0
GPU_PROVISIONER_VERSION ?= 0.3.5
RAGENGINE_IMG_NAME ?= ragengine
IMG_TAG ?= $(subst v,,$(VERSION))

ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(abspath $(ROOT_DIR)/bin)

TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/bin)

GOLANGCI_LINT_VER := latest
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER))

E2E_TEST_BIN := e2e.test
E2E_TEST := $(BIN_DIR)/$(E2E_TEST_BIN)

RAGENGINE_E2E_TEST_BIN := rage2e.test
RAGENGINE_E2E_TEST := $(BIN_DIR)/$(RAGENGINE_E2E_TEST_BIN)

GINKGO_VER := v2.19.0
GINKGO_BIN := ginkgo
GINKGO := $(TOOLS_BIN_DIR)/$(GINKGO_BIN)-$(GINKGO_VER)
TEST_SUITE ?= gpuprovisioner

AZURE_SUBSCRIPTION_ID ?= $(AZURE_SUBSCRIPTION_ID)
AZURE_LOCATION ?= eastus
AKS_K8S_VERSION ?= 1.31.10
AZURE_CLUSTER_NAME ?= kaito-demo
AZURE_RESOURCE_GROUP ?= demo
AZURE_RESOURCE_GROUP_MC=MC_$(AZURE_RESOURCE_GROUP)_$(AZURE_CLUSTER_NAME)_$(AZURE_LOCATION)
GPU_PROVISIONER_NAMESPACE ?= gpu-provisioner
GPU_PROVISIONER_NAME ?= gpu-provisioner
KAITO_NAMESPACE ?= kaito-workspace
KAITO_RAGENGINE_NAMESPACE ?= kaito-ragengine
GPU_PROVISIONER_MSI_NAME ?= gpuprovisionerIdentity

## Azure Karpenter parameters
KARPENTER_NAMESPACE ?= karpenter
KARPENTER_SA_NAME ?= karpenter-sa
KARPENTER_VERSION ?= 0.5.1
AZURE_KARPENTER_MSI_NAME ?= azkarpenterIdentity

AI_MODELS_REGISTRY ?= modelregistry.azurecr.io
AI_MODELS_REGISTRY_SECRET ?= modelregistry
SUPPORTED_MODELS_YAML_PATH ?= $(ROOT_DIR)/presets/workspace/models/supported_models.yaml

## AWS parameters
CLUSTER_CONFIG_FILE ?= ./docs/aws/clusterconfig.yaml.template
RENDERED_CLUSTER_CONFIG_FILE ?= ./docs/aws/clusterconfig.yaml
AWS_KARPENTER_VERSION ?=1.0.8

# Scripts
GO_INSTALL := ./hack/go-install.sh

BUILD_FLAGS ?=

# Extra arguments for commands
HELM_INSTALL_EXTRA_ARGS ?=

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## --------------------------------------
## Tooling Binaries
## --------------------------------------

##@ Tooling Binaries

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download and install golangci-lint locally.

.PHONY: ginkgo
ginkgo: $(GOLANGCI_LINT) ## Download and install ginkgo locally.

$(GOLANGCI_LINT): ## Download and install golangci-lint locally.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/golangci/golangci-lint/cmd/golangci-lint $(GOLANGCI_LINT_BIN) $(GOLANGCI_LINT_VER)

$(GINKGO): ## Download and install ginkgo locally.
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) github.com/onsi/ginkgo/v2/ginkgo $(GINKGO_BIN) $(GINKGO_VER)

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

## --------------------------------------
## Development
## --------------------------------------

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole, and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	cp config/crd/bases/kaito.sh_workspaces.yaml charts/kaito/workspace/crds/
	cp config/crd/bases/kaito.sh_ragengines.yaml charts/kaito/ragengine/crds/

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: compare-model-configs
compare-model-configs: ## Compare supported_models.yaml with ConfigMap template (ignoring comments).
	@./hack/compare_model_configs.sh

## --------------------------------------
## Unit Tests
## --------------------------------------

##@ Unit Tests

.PHONY: unit-test
unit-test: ## Run unit tests.
	go test -v $(shell go list ./pkg/... ./api/... | \
	grep -v -e /vendor -e /api/v1alpha1/zz_generated.deepcopy.go -e /api/v1beta1/zz_generated.deepcopy.go -e /pkg/utils/test/...) \
	-race -coverprofile=coverage.txt -covermode=atomic
	go tool cover -func=coverage.txt

.PHONY: rag-service-test
rag-service-test: ## Run RAG Engine service tests with pytest.
	pip install -r presets/ragengine/requirements-test.txt
	pip install pytest-cov
	pytest --cov -o log_cli=true -o log_cli_level=INFO presets/ragengine/tests

.PHONY: tuning-metrics-server-test
tuning-metrics-server-test: ## Run Tuning Metrics Server tests with pytest.
	pip install -r ./presets/workspace/dependencies/requirements-test.txt
	pip install pytest-cov
	pytest --cov -o log_cli=true -o log_cli_level=INFO presets/workspace/tuning/text-generation/metrics

## --------------------------------------
## E2E Tests
## --------------------------------------

##@ E2E Tests

.PHONY: interference-api-e2e
inference-api-e2e: ## Run inference API e2e tests with pytest.
	pip install -r ./presets/workspace/dependencies/requirements-test.txt
	pip install pytest-cov
	pytest --cov -o log_cli=true -o log_cli_level=INFO presets/workspace/inference/vllm
	pytest --cov -o log_cli=true -o log_cli_level=INFO presets/workspace/inference/text-generation

# Ginkgo configurations
GINKGO_FOCUS ?=
GINKGO_SKIP ?=
GINKGO_LABEL ?= !A100Required
GINKGO_NODES ?= 2
GINKGO_NO_COLOR ?= false
GINKGO_TIMEOUT ?= 120m
GINKGO_ARGS ?= --label-filter="$(GINKGO_LABEL)" -focus="$(GINKGO_FOCUS)" -skip="$(GINKGO_SKIP)" -nodes=$(GINKGO_NODES) -no-color=$(GINKGO_NO_COLOR) -timeout=$(GINKGO_TIMEOUT) --fail-fast

.PHONY: $(E2E_TEST)
$(E2E_TEST): ## Build the e2e test binary without running it.
	(cd test/e2e && go test -c . -o $(E2E_TEST))

.PHONY: $(RAGENGINE_E2E_TEST)
$(RAGENGINE_E2E_TEST): ## Build the RAG Engine e2e test binary without running it.
	(cd test/rage2e && go test -c . -o $(RAGENGINE_E2E_TEST))

.PHONY: kaito-workspace-e2e-test
kaito-workspace-e2e-test: $(E2E_TEST) $(GINKGO) ## Run e2e tests for Kaito Workspace.
	AI_MODELS_REGISTRY_SECRET=$(AI_MODELS_REGISTRY_SECRET) \
 	AI_MODELS_REGISTRY=$(AI_MODELS_REGISTRY) GPU_PROVISIONER_NAMESPACE=$(GPU_PROVISIONER_NAMESPACE) GPU_PROVISIONER_NAME=$(GPU_PROVISIONER_NAME) \
 	KARPENTER_NAMESPACE=$(KARPENTER_NAMESPACE) KAITO_NAMESPACE=$(KAITO_NAMESPACE) TEST_SUITE=$(TEST_SUITE) \
	SUPPORTED_MODELS_YAML_PATH=$(SUPPORTED_MODELS_YAML_PATH) \
 	$(GINKGO) -v -trace $(GINKGO_ARGS) $(E2E_TEST)

.PHONY: kaito-ragengine-e2e-test
kaito-ragengine-e2e-test: $(RAGENGINE_E2E_TEST) $(GINKGO) ## Run e2e tests for Kaito RAG Engine.
	AI_MODELS_REGISTRY_SECRET=$(AI_MODELS_REGISTRY_SECRET) \
	AI_MODELS_REGISTRY=$(AI_MODELS_REGISTRY) GPU_PROVISIONER_NAMESPACE=$(GPU_PROVISIONER_NAMESPACE)  GPU_PROVISIONER_NAME=$(GPU_PROVISIONER_NAME) KAITO_NAMESPACE=$(KAITO_NAMESPACE) \
	KARPENTER_NAMESPACE=$(KARPENTER_NAMESPACE) KAITO_RAGENGINE_NAMESPACE=$(KAITO_RAGENGINE_NAMESPACE) TEST_SUITE=$(TEST_SUITE) \
	SUPPORTED_MODELS_YAML_PATH=$(SUPPORTED_MODELS_YAML_PATH) \
	$(GINKGO) -v -trace $(GINKGO_ARGS) $(RAGENGINE_E2E_TEST)

## --------------------------------------
## Azure Setup
## --------------------------------------

##@ Azure Setup

.PHONY: create-rg
create-rg: ## Create Azure resource group.
	az group create --name $(AZURE_RESOURCE_GROUP) --location $(AZURE_LOCATION) -o none

.PHONY: create-acr
create-acr: ## Create Azure container registry and login into it.
	az acr create --name $(AZURE_ACR_NAME) --resource-group $(AZURE_RESOURCE_GROUP) --sku Standard --admin-enabled -o none
	az acr login  --name $(AZURE_ACR_NAME)

.PHONY: create-aks-cluster
create-aks-cluster: ## Create an AKS cluster with MSI, OIDC, and workload identity enabled.
	az aks create  --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) \
	--location $(AZURE_LOCATION) --attach-acr $(AZURE_ACR_NAME) \
	--kubernetes-version $(AKS_K8S_VERSION) --node-count 1 --generate-ssh-keys  \
	--enable-managed-identity --enable-workload-identity --enable-oidc-issuer --node-vm-size Standard_D2d_v4 -o none
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) --overwrite-existing

.PHONY: create-aks-cluster-with-kaito
create-aks-cluster-with-kaito: ## Create an AKS cluster with MSI, OIDC, and Kaito enabled.
	az aks create  --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) \
	--location $(AZURE_LOCATION) --attach-acr $(AZURE_ACR_NAME) \
	--kubernetes-version $(AKS_K8S_VERSION) --node-count 1 --generate-ssh-keys  \
	--enable-managed-identity --enable-workload-identity --enable-oidc-issuer -o none
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) --overwrite-existing

.PHONY: create-aks-cluster-for-karpenter
create-aks-cluster-for-karpenter: ## Create an AKS cluster with MSI, Cillium, OIDC, and workload identity enabled.
	az aks create --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) \
    --location $(AZURE_LOCATION) --attach-acr $(AZURE_ACR_NAME) --node-vm-size "Standard_D2d_v4" \
    --kubernetes-version $(AKS_K8S_VERSION) --node-count 3 --generate-ssh-keys \
    --network-plugin azure --network-plugin-mode overlay --network-dataplane cilium \
    --enable-managed-identity --enable-oidc-issuer --enable-workload-identity -o none
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP) --overwrite-existing

## --------------------------------------
## AWS Setup
## --------------------------------------

##@ AWS Setup

.PHONY: mktemp
mktemp: ## Create a temporary file.
	$(eval TEMPOUT := $(shell mktemp))

.PHONY: deploy-aws-cloudformation
deploy-aws-cloudformation: mktemp ## Deploy AWS CloudFormation stack.
	curl -fsSL https://raw.githubusercontent.com/aws/karpenter-provider-aws/v"${AWS_KARPENTER_VERSION}"/website/content/en/preview/getting-started/getting-started-with-karpenter/cloudformation.yaml  > "${TEMPOUT}"

	aws cloudformation deploy \
	--stack-name "Karpenter-${AWS_CLUSTER_NAME}" \
	--template-file "${TEMPOUT}" \
	--capabilities CAPABILITY_NAMED_IAM \
	--parameter-overrides "ClusterName=${AWS_CLUSTER_NAME}"

.PHONY: create-eks-cluster
create-eks-cluster: ## Create an EKS cluster.
	@envsubst < $(CLUSTER_CONFIG_FILE) > $(RENDERED_CLUSTER_CONFIG_FILE)

	eksctl create cluster -f $(RENDERED_CLUSTER_CONFIG_FILE)

## --------------------------------------
## Docker
## --------------------------------------

##@ Docker

BUILDX_BUILDER_NAME ?= img-builder
OUTPUT_TYPE ?= type=registry
QEMU_VERSION ?= 7.2.0-1
ARCH ?= amd64,arm64
BUILDKIT_VERSION ?= v0.18.1

RAGENGINE_IMAGE_NAME ?= ragengine
RAGENGINE_IMAGE_TAG ?= v0.0.1

RAGENGINE_SERVICE_IMG_NAME ?= kaito-rag-service
RAGENGINE_SERVICE_IMG_TAG ?= v0.0.1

E2E_IMAGE_NAME ?= kaito-e2e
E2E_IMAGE_TAG ?= v0.0.1


.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support.
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --name $(BUILDX_BUILDER_NAME) --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-workspace
docker-build-workspace: docker-buildx ## Build Docker image for workspace.
	docker buildx build \
		--file ./docker/workspace/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/$(IMG_NAME):$(IMG_TAG) .

.PHONY: docker-build-ragengine
docker-build-ragengine: docker-buildx ## Build Docker image for RAG Engine.
	docker buildx build \
		--file ./docker/ragengine/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/$(RAGENGINE_IMAGE_NAME):$(IMG_TAG) .

.PHONY: docker-build-rag-service
docker-build-ragservice: docker-buildx ## Build Docker image for RAG Engine service.
	docker buildx build \
        --platform="linux/$(ARCH)" \
        --output=$(OUTPUT_TYPE) \
        --file ./docker/ragengine/service/Dockerfile \
        --pull \
		$(BUILD_FLAGS) \
        --tag $(REGISTRY)/$(RAGENGINE_SERVICE_IMG_NAME):$(RAGENGINE_SERVICE_IMG_TAG) .

.PHONY: docker-build-adapter
docker-build-adapter: docker-buildx ## Build Docker images for adapters.
	docker buildx build \
		--build-arg ADAPTER_PATH=docker/adapters/adapter1 \
		--file ./docker/adapters/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/e2e-adapter:0.0.1 .
	docker buildx build \
		--build-arg ADAPTER_PATH=docker/adapters/adapter2 \
		--file ./docker/adapters/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/e2e-adapter2:0.0.1 .
	docker buildx build \
		--build-arg ADAPTER_PATH=docker/adapters/adapter-phi-3-mini-pycoder \
		--file ./docker/adapters/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/adapter-phi-3-mini-pycoder:0.0.1 .

.PHONY: docker-build-dataset
docker-build-dataset: docker-buildx ## Build Docker images for datasets.
	docker buildx build \
		--build-arg ADAPTER_PATH=docker/datasets/dataset1 \
		--file ./docker/datasets/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/e2e-dataset:0.0.1 .
	docker buildx build \
		--build-arg ADAPTER_PATH=docker/datasets/dataset2 \
		--file ./docker/datasets/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		--tag $(REGISTRY)/e2e-dataset2:0.0.1 .

.PHONY: docker-build-llm-reference-preset
docker-build-llm-reference-preset: docker-buildx ## Build Docker image for LLM reference preset.
	docker buildx build \
		-t ghcr.io/kaito-repo/kaito/llm-reference-preset:$(VERSION) \
		-t ghcr.io/kaito-repo/kaito/llm-reference-preset:latest \
		-f docs/custom-model-integration/Dockerfile.reference \
		$(BUILD_FLAGS) \
		--build-arg MODEL_TYPE=text-generation \
		--build-arg VERSION=$(VERSION) .

## --------------------------------------
## Kaito Installation
## --------------------------------------

##@ Kaito Installation

.PHONY: prepare-kaito-addon-identity
prepare-kaito-addon-identity: ## Create Azure identity and federated credential for Kaito addon.
	IDENTITY_PRINCIPAL_ID=$(shell az identity show --name "ai-toolchain-operator-$(AZURE_CLUSTER_NAME)" -g "$(AZURE_RESOURCE_GROUP_MC)"  --query 'principalId');\
	az role assignment create --assignee $$IDENTITY_PRINCIPAL_ID --scope "/subscriptions/$(AZURE_SUBSCRIPTION_ID)/resourceGroups/$(AZURE_RESOURCE_GROUP_MC)"  --role "Contributor"

	AKS_OIDC_ISSUER=$(shell az aks show -n "$(AZURE_CLUSTER_NAME)" -g "$(AZURE_RESOURCE_GROUP_MC)" --query 'oidcIssuerProfile.issuerUrl');\
	az identity federated-credential create --name gpu-federated-cred --identity-name "ai-toolchain-operator-$(AZURE_CLUSTER_NAME)" \
    -g "$(AZURE_RESOURCE_GROUP)" --issuer $$AKS_OIDC_ISSUER \
    --subject system:serviceaccount:"$(KAITO_NAMESPACE):kaito-gpu-provisioner" --audience api://AzureADTokenExchange

.PHONY: az-patch-install-helm
az-patch-install-helm: ## Install Kaito workspace Helm chart and set Azure client env vars and settings in Helm values.
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP)

	yq -i '(.image.repository)                                              = "$(REGISTRY)/workspace"'                    ./charts/kaito/workspace/values.yaml
	yq -i '(.image.tag)                                                     = "$(IMG_TAG)"'                               ./charts/kaito/workspace/values.yaml
	yq -i '(.clusterName)                                                   = "$(AZURE_CLUSTER_NAME)"'                    ./charts/kaito/workspace/values.yaml

	helm install kaito-workspace ./charts/kaito/workspace --namespace $(KAITO_NAMESPACE) --create-namespace $(HELM_INSTALL_EXTRA_ARGS)

.PHONY: az-patch-install-ragengine-helm
az-patch-install-ragengine-helm: ## Install Kaito RAG Engine Helm chart and set Azure client env vars and settings in Helm values.
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP)

	yq -i '(.image.repository)                                              = "$(REGISTRY)/ragengine"'                    ./charts/kaito/ragengine/values.yaml
	yq -i '(.image.tag)                                                     = "$(IMG_TAG)"'                               ./charts/kaito/ragengine/values.yaml
	yq -i '(.clusterName)                                                   = "$(AZURE_CLUSTER_NAME)"'                    ./charts/kaito/ragengine/values.yaml

	helm install kaito-ragengine ./charts/kaito/ragengine --namespace $(KAITO_RAGENGINE_NAMESPACE) --create-namespace $(HELM_INSTALL_EXTRA_ARGS)

.PHONY: az-patch-install-ragengine-helm-e2e
az-patch-install-ragengine-helm-e2e: ## Install Kaito RAG Engine Helm chart for e2e tests and set Azure client env vars and settings in Helm values.
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP)

	yq -i '(.image.repository)                                              = "$(REGISTRY)/ragengine"'                    ./charts/kaito/ragengine/values.yaml
	yq -i '(.image.tag)                                                     = "$(IMG_TAG)"'                               ./charts/kaito/ragengine/values.yaml
	yq -i '(.clusterName)                                                   = "$(AZURE_CLUSTER_NAME)"'                    ./charts/kaito/ragengine/values.yaml
	yq -i '(.presetRagRegistryName)                                         = "$(REGISTRY)"'                              ./charts/kaito/ragengine/values.yaml
	yq -i '(.presetRagImageName)                                         	= "$(RAGENGINE_SERVICE_IMG_NAME)"'            ./charts/kaito/ragengine/values.yaml
	yq -i '(.presetRagImageTag)                                         	= "$(RAGENGINE_SERVICE_IMG_TAG)"'             ./charts/kaito/ragengine/values.yaml

	helm install kaito-ragengine ./charts/kaito/ragengine --namespace $(KAITO_RAGENGINE_NAMESPACE) --create-namespace $(HELM_INSTALL_EXTRA_ARGS)

.PHONY: aws-patch-install-helm
aws-patch-install-helm: ## Install Kaito workspace Helm chart and set AWS env vars and settings in Helm values.
	yq -i '(.image.repository)                                              = "$(REGISTRY)/workspace"'                    	./charts/kaito/workspace/values.yaml
	yq -i '(.image.tag)                                                     = "$(IMG_TAG)"'                               	./charts/kaito/workspace/values.yaml
	yq -i '(.clusterName)                                                   = "$(AWS_CLUSTER_NAME)"'                    		./charts/kaito/workspace/values.yaml
	yq -i '(.cloudProviderName)                                             = "aws"'                                        ./charts/kaito/workspace/values.yaml

	helm install kaito-workspace ./charts/kaito/workspace --namespace $(KAITO_NAMESPACE) --create-namespace $(HELM_INSTALL_EXTRA_ARGS)

generate-identities: ## Create identities for the provisioner component.
	./hack/deploy/generate-identities.sh \
	$(AZURE_CLUSTER_NAME) $(AZURE_RESOURCE_GROUP) $(TEST_SUITE) $(AZURE_SUBSCRIPTION_ID)

## --------------------------------------
## GPU Provisioner Installation
## --------------------------------------

##@ GPU Provisioner Installation

.PHONY: gpu-provisioner-helm
gpu-provisioner-helm: ## Install GPU provisioner Helm chart for Azure cluster and set Azure client env vars and settings in Helm values.
	curl -sO https://raw.githubusercontent.com/Azure/gpu-provisioner/main/hack/deploy/configure-helm-values.sh
	chmod +x ./configure-helm-values.sh && ./configure-helm-values.sh $(AZURE_CLUSTER_NAME) \
	$(AZURE_RESOURCE_GROUP) $(GPU_PROVISIONER_MSI_NAME)

	helm install $(GPU_PROVISIONER_NAMESPACE) \
	--values gpu-provisioner-values.yaml \
	--set settings.azure.clusterName=$(AZURE_CLUSTER_NAME) \
	--namespace $(GPU_PROVISIONER_NAMESPACE) --create-namespace \
	https://github.com/Azure/gpu-provisioner/raw/gh-pages/charts/gpu-provisioner-$(GPU_PROVISIONER_VERSION).tgz $(HELM_INSTALL_EXTRA_ARGS)

	kubectl wait --for=condition=available deploy "gpu-provisioner" -n gpu-provisioner --timeout=300s

## --------------------------------------
## Karpenter Installation
## --------------------------------------

##@ Karpenter Installation

.PHONY: azure-karpenter-helm
azure-karpenter-helm: ## Install Azure Karpenter Helm chart and set Azure client env vars and settings in Helm values.
	curl -sO https://raw.githubusercontent.com/Azure/karpenter-provider-azure/main/hack/deploy/configure-values.sh
	chmod +x ./configure-values.sh && ./configure-values.sh $(AZURE_CLUSTER_NAME) \
	$(AZURE_RESOURCE_GROUP) $(KARPENTER_SA_NAME) $(AZURE_KARPENTER_MSI_NAME)

	helm upgrade --install karpenter oci://mcr.microsoft.com/aks/karpenter/karpenter \
	--version "$(KARPENTER_VERSION)" \
    --namespace "$(KARPENTER_NAMESPACE)" --create-namespace \
    --values karpenter-values.yaml \
    --set controller.resources.requests.cpu=1 \
    --set controller.resources.requests.memory=1Gi \
    --set controller.resources.limits.cpu=1 \
    --set controller.resources.limits.memory=1Gi

	kubectl wait --for=condition=available deploy "karpenter" -n karpenter --timeout=300s

.PHONY: aws-karpenter-helm
aws-karpenter-helm: ## Install AWS Karpenter Helm chart and set AWS env vars and settings in Helm values.
	helm upgrade --install karpenter oci://public.ecr.aws/karpenter/karpenter \
	--version "${AWS_KARPENTER_VERSION}" \
	--namespace "${KARPENTER_NAMESPACE}" --create-namespace \
	--set "settings.clusterName=${AWS_CLUSTER_NAME}" \
	--set "settings.interruptionQueue=${AWS_CLUSTER_NAME}" \
	--set controller.resources.requests.cpu=1 \
	--set controller.resources.requests.memory=1Gi \
	--set controller.resources.limits.cpu=1 \
	--set controller.resources.limits.memory=1Gi

	kubectl wait --for=condition=available deploy "karpenter" -n ${KARPENTER_NAMESPACE} --timeout=300s

## --------------------------------------
## Build
## --------------------------------------

##@ Build

.PHONY: build-workspace
build-workspace: manifests generate fmt vet ## Build manager binary.
	go build -o bin/workspace-manager cmd/workspace/*.go

.PHONY: run-workspace
run-workspace: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/workspace/main.go

.PHONY: localbin
localbin: $(LOCALBIN) ## Create folder for installing local binaries.

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN): ## Create folder for installing local binaries.
	mkdir -p $(LOCALBIN)

## --------------------------------------
## RAG Engine
## --------------------------------------

##@ RAG Engine

.PHONY: build-ragengine
build-ragengine: manifests generate fmt vet ## Build RAG Engine binary.
	go build -o bin/rag-engine-manager cmd/ragengine/*.go

.PHONY: run-ragengine
run-ragengine: manifests generate fmt vet ## Run RAG Engine controller from command line.
	go run ./cmd/ragengine/main.go


## --------------------------------------
## Deployment
## --------------------------------------

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

## Tool Binaries
KUBECTL ?= kubectl
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.15.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

## --------------------------------------
## Linting
## --------------------------------------

##@ Linting

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run -v

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

## --------------------------------------
## Release
## To create a release, run `make release VERSION=vx.y.z`
## --------------------------------------

##@ Release

.PHONY: release-manifest
release-manifest: ## Update manifest and Helm charts for release.
	@sed -i -e 's/^VERSION ?= .*/VERSION ?= ${VERSION}/' ./Makefile
	@sed -i -e "s/version: .*/version: ${IMG_TAG}/" ./charts/kaito/workspace/Chart.yaml
	@sed -i -e "s/appVersion: .*/appVersion: ${IMG_TAG}/" ./charts/kaito/workspace/Chart.yaml
	@sed -i -e "s/tag: .*/tag: ${IMG_TAG}/" ./charts/kaito/workspace/values.yaml
	@sed -i -e 's/IMG_TAG=.*/IMG_TAG=${IMG_TAG}/' ./charts/kaito/workspace/README.md
	@sed -i -e "s/version: .*/version: ${IMG_TAG}/" ./charts/kaito/ragengine/Chart.yaml
	@sed -i -e "s/appVersion: .*/appVersion: ${IMG_TAG}/" ./charts/kaito/ragengine/Chart.yaml
	@sed -i -e "s/tag: .*/tag: ${IMG_TAG}/" ./charts/kaito/ragengine/values.yaml
	@sed -i -e "s/presetRagImageTag: .*/presetRagImageTag: ${IMG_TAG}/" ./charts/kaito/ragengine/values.yaml
	@sed -i -e 's/IMG_TAG=.*/IMG_TAG=${IMG_TAG}/' ./charts/kaito/ragengine/README.md

	git checkout -b release-${VERSION}
	git add ./Makefile ./charts/kaito/workspace/Chart.yaml ./charts/kaito/workspace/values.yaml ./charts/kaito/workspace/README.md ./charts/kaito/ragengine/Chart.yaml ./charts/kaito/ragengine/values.yaml ./charts/kaito/ragengine/README.md
	git commit -s -m "release: update manifest and helm charts for ${VERSION}"

## --------------------------------------
## Post Release Doc Update
## To update documents after a release, run `make post-release-doc-update VERSION=vx.y.z`
## --------------------------------------

##@ Post Release Doc Update

.PHONY: post-release-doc-update
post-release-doc-update:
	@sed -i -e 's/export KAITO_WORKSPACE_VERSION=.*/export KAITO_WORKSPACE_VERSION=${IMG_TAG}/' ./website/docs/installation.md
	@sed -i -e 's/$(shell grep 'default' ./terraform/variables.tf | sort | uniq -c | sort -nr | head -n1 | sed -E 's/^[ ]*[0-9]+[ ]+//')/default     = "${IMG_TAG}"/' ./terraform/variables.tf


## --------------------------------------
## Cleanup
## --------------------------------------

##@ Cleanup

.PHONY: clean
clean: clean-bin ## Remove all generated and temporary files.

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries.
	@rm -rf $(BIN_DIR)

## --------------------------------------
## Help
## --------------------------------------

##@ Help:

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[0-9a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
