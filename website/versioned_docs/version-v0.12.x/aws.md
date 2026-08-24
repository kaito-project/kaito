---
title: AWS Setup
---

This guide covers setting up auto-provisioning capabilities for KAITO on Amazon Elastic Kubernetes Service (EKS). Auto-provisioning lets KAITO automatically create GPU nodes when your AI workloads need them. The steps below follow the upstream [Getting Started with Karpenter](https://karpenter.sh/docs/getting-started/getting-started-with-karpenter/) guide and add the KAITO-specific configuration.

## Prerequisites

- An EKS cluster with the KAITO workspace controller installed
  - See [Step 1](#step-1-create-the-eks-cluster) to create an EKS cluster
  - See [Installation](installation) to install the KAITO workspace controller
- [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) for managing AWS resources
- [eksctl](https://eksctl.io/installation/) (>= v0.202.0) to create and manage EKS clusters
- [Helm](https://helm.sh) to install the controllers
- [kubectl](https://kubernetes.io/docs/tasks/tools/) to interact with your cluster

Configure the AWS CLI with a principal that can create EKS clusters, CloudFormation stacks, IAM roles, and EC2 resources, then verify it authenticates:

```bash
aws sts get-caller-identity
```

## Understanding Auto-Provisioning on AWS

KAITO uses [Karpenter (karpenter-provider-aws)](https://github.com/aws/karpenter-provider-aws) to automatically provision GPU nodes. Karpenter:

- Creates new GPU nodes when a `Workspace` requests a specific instance type
- Supports various AWS GPU instances (g4, g5, g6, p4, p5 series, etc.)
- Manages node lifecycle (consolidation, expiration) based on workload demand
- Uses EKS Pod Identity / IAM for secure access

For each `Workspace`, KAITO creates a Karpenter `NodePool` that references an `EC2NodeClass` named `default`. Karpenter then provisions the actual GPU nodes from that `NodePool`. When you configure the workspace controller with `nodeProvisioner=karpenter` and `karpenterProvider=aws` (Step 3 below), KAITO creates the `default` `EC2NodeClass` for you automatically — no manual node class creation is required.

:::note
Alternative: If you already have GPU nodes or manage them separately, use the [bring your own GPU nodes approach](./installation.md#option-2-bring-your-own-gpu-nodes) instead.
:::

## Set Up Auto-Provisioning

Setting up auto-provisioning has two parts:

1. **Create the EKS cluster and install Karpenter** — this is standard Karpenter setup. Follow the upstream [Getting Started with Karpenter](https://karpenter.sh/docs/getting-started/getting-started-with-karpenter/) guide so you always track the latest supported procedure, plus the one KAITO-specific requirement noted in Step 1.
2. **Apply the KAITO-specific configuration** — point the KAITO workspace controller at Karpenter and supply the `EC2NodeClass` (Step 2 below).

:::tip Prefer `make` targets?
If you'd rather not run each command by hand, the KAITO repository ships `make` targets that automate Step 1. See [Alternative: set up with KAITO make targets](#alternative-set-up-with-kaito-make-targets).
:::

### Step 1: Create the EKS cluster and install Karpenter

Follow [Getting Started with Karpenter](https://karpenter.sh/docs/getting-started/getting-started-with-karpenter/) to create the EKS cluster, provision the Karpenter IAM roles and infrastructure, and install the Karpenter controller with its Helm chart. That guide also sets up the `karpenter.sh/discovery` tag and the `KarpenterNodeRole-<cluster>` IAM role that the `EC2NodeClass` in Step 2 references.

:::important KAITO requires the `StaticCapacity` feature gate
When you install the Karpenter Helm chart, enable the `StaticCapacity` feature gate:

```bash
--set settings.featureGates.staticCapacity=true
```

KAITO provisions GPU nodes by setting a fixed `spec.replicas` on the `NodePool` it creates for each `Workspace`, and that field is only honored when the `StaticCapacity` gate is on. Without it, Karpenter ignores `replicas`, no `NodeClaim` is created, and the `Workspace` stays `Pending`.
:::

If this is the first time you use EC2 Spot in this account, create the Spot service-linked role (safe to re-run):

```bash
aws iam create-service-linked-role --aws-service-name spot.amazonaws.com || true
```

### Step 2: Configure the KAITO Workspace Controller to Use Karpenter

The KAITO workspace controller must run with `nodeProvisioner=karpenter`. Update your [existing installation](./installation.md) with `helm upgrade`, reusing the same `kaito/workspace` chart from the Helm repository you added during [Installation](./installation.md).

Unlike Azure, the AWS `EC2NodeClass` configuration is not built into the chart, so supply it in this step together with the Karpenter settings. The `role` must match the `KarpenterNodeRole-$AWS_CLUSTER_NAME` IAM role, and the subnet/security-group selectors use the `karpenter.sh/discovery=$AWS_CLUSTER_NAME` tag — both created for you in Step 1:

```bash
cat <<EOF > kaito-aws-values.yaml
nodeProvisioner: karpenter
karpenterProvider: aws
cloudProviderName: aws
clusterName: ${AWS_CLUSTER_NAME}
karpenterProviders:
  aws:
    group: karpenter.k8s.aws
    kind: EC2NodeClass
    version: v1
    resourceName: ec2nodeclasses
    nodeClasses:
      - name: default
        default: true
        spec:
          role: KarpenterNodeRole-${AWS_CLUSTER_NAME}
          amiSelectorTerms:
            - alias: al2023@latest
          blockDeviceMappings:
            - deviceName: /dev/xvda
              ebs:
                volumeSize: 300Gi
                volumeType: gp3
                deleteOnTermination: true
          subnetSelectorTerms:
            - tags:
                karpenter.sh/discovery: ${AWS_CLUSTER_NAME}
          securityGroupSelectorTerms:
            - tags:
                karpenter.sh/discovery: ${AWS_CLUSTER_NAME}
EOF

helm upgrade kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --reuse-values \
  -f kaito-aws-values.yaml
```

Setting `nodeProvisioner=karpenter` does two things: it renders the `kaito-nodeclasses` ConfigMap (here the `EC2NodeClass` you defined above), and it starts the controller with `--node-provisioner=karpenter`. The upgrade re-renders the ConfigMap and triggers a controller rollout; on startup the new pod reads that ConfigMap and creates the `default` `EC2NodeClass`, so the node class only appears after this step.

### Alternative: set up with KAITO make targets

Instead of running the Step 1 commands by hand, you can use the `make` targets shipped in the KAITO repository. They wrap the same upstream Karpenter CloudFormation template and Helm chart, so the result matches the official guide. Choose this path only if you're comfortable cloning the repo.

Clone the repository and change into it:

```bash
git clone https://github.com/kaito-project/kaito.git
cd kaito
```

Set the environment variables consumed by the `make` targets and by `eksctl`/`aws` (keep the shell open for all steps, and re-export them if you start a new shell):

```bash
export AWS_CLUSTER_NAME=kaito-aws
export AWS_REGION=us-west-2
export AWS_DEFAULT_REGION=$AWS_REGION       # used by the AWS CLI
export AWS_PARTITION=aws                     # aws, aws-cn, or aws-us-gov
export AWS_K8S_VERSION=1.36
export KARPENTER_NAMESPACE=kube-system
export ENABLE_ZONAL_SHIFT=false
export AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
```

Create the cluster and install Karpenter:

```bash
make deploy-aws-cloudformation
make create-eks-cluster
make aws-karpenter-helm
```

- `make deploy-aws-cloudformation` downloads the official Karpenter CloudFormation template (pinned by `AWS_KARPENTER_VERSION`, default `1.13.0`) and creates the Karpenter IAM roles and split controller policies.
- `make create-eks-cluster` renders [`examples/aws/clusterconfig.yaml.template`](https://github.com/kaito-project/kaito/blob/main/examples/aws/clusterconfig.yaml.template) with your environment variables and runs `eksctl create cluster`. The template provisions OIDC, EKS Pod Identity, an `AmazonLinux2023` managed node group for the controllers, zonal-shift config, and the `karpenter.sh/discovery` tag Karpenter uses to discover subnets and security groups.
- `make aws-karpenter-helm` installs the Karpenter controller and already enables the `StaticCapacity` feature gate (`settings.featureGates.staticCapacity=true`) that KAITO requires.

If you already have an EKS cluster, connect to it instead of running the create targets:

```bash
aws eks update-kubeconfig --name $AWS_CLUSTER_NAME --region $AWS_REGION
```

Then continue with [Step 2: Configure the KAITO Workspace Controller to Use Karpenter](#step-2-configure-the-kaito-workspace-controller-to-use-karpenter) above.

## Verify Setup

Check that Karpenter and the KAITO workspace controller are running:

```bash
# Karpenter
helm list -n "${KARPENTER_NAMESPACE}"
kubectl get pods -n "${KARPENTER_NAMESPACE}"
kubectl describe deploy karpenter -n "${KARPENTER_NAMESPACE}"

# KAITO workspace controller
kubectl get pods -n kaito-workspace
kubectl describe deploy kaito-workspace -n kaito-workspace

# EC2NodeClass auto-created by KAITO from the config you supplied in Step 3
kubectl get ec2nodeclass default
```

## Using Auto-Provisioning

Create a `Workspace` that requests a GPU instance type. Karpenter provisions a matching node automatically:

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "g5.4xlarge" # Will trigger node creation
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

Apply the workspace:

```bash
kubectl apply -f phi-4-workspace.yaml
```

Track progress until `WORKSPACESUCCEEDED` becomes `True`:

```bash
kubectl get workspace workspace-phi-4-mini
```

Once ready, test the inference endpoint from inside the cluster:

```bash
export CLUSTERIP=$(kubectl get svc workspace-phi-4-mini -o jsonpath="{.spec.clusterIP}")
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- \
  curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"phi-4-mini-instruct","messages":[{"role":"user","content":"Say hello in one short sentence."}],"max_tokens":40}'
```

## Supported AWS GPU Instance Types

KAITO supports various AWS GPU SKUs; see the [supported options here](https://github.com/kaito-project/kaito/blob/main/pkg/sku/aws_sku_handler.go).

For the complete list and specifications, see the [AWS GPU instance documentation](https://docs.aws.amazon.com/dlami/latest/devguide/gpu.html).

:::note
KAITO only supports NVIDIA GPUs with CUDA compute capability **>= 8.0** (Ampere and newer, such as A10G, A100, L4, H100, H200). Older architectures such as **NVIDIA T4** (Turing, 7.5), **NVIDIA V100** (Volta, 7.0), **NVIDIA M60** (Maxwell, 5.2), and **NVIDIA K80** (Kepler, 3.7) are **not supported**. This means instance families such as `g4dn.*`, `g5g.*` (T4), `p3.*`, `p3dn.*` (V100), `g3s.*` (M60), and `p2.*` (K80) cannot be used with KAITO.
:::

## Clean Up

To avoid ongoing charges, remove the workloads, controllers, and cluster:

```bash
# Remove workspaces (this also removes the GPU nodes Karpenter provisioned)
kubectl delete workspace --all

# Uninstall the controllers
helm uninstall kaito-workspace -n kaito-workspace
helm uninstall karpenter -n "${KARPENTER_NAMESPACE}"

# Delete the Karpenter IAM stack and any leftover launch templates
aws cloudformation delete-stack --stack-name "Karpenter-${AWS_CLUSTER_NAME}"
aws ec2 describe-launch-templates --filters "Name=tag:karpenter.k8s.aws/cluster,Values=${AWS_CLUSTER_NAME}" \
  | jq -r ".LaunchTemplates[].LaunchTemplateName" \
  | xargs -I{} aws ec2 delete-launch-template --launch-template-name {}

# Delete the cluster
eksctl delete cluster --name "${AWS_CLUSTER_NAME}"
```
