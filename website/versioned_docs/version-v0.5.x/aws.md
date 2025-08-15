---
title: AWS Deployment
---

# Installation using AWS EKS cluster

Before you begin, ensure you have the following tools installed:
- [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) to provision AWS resources
- [Eksctl](https://eksctl.io/installation/) (>= v0.191.0) to create and manage clusters on EKS
- [Helm](https://helm.sh) to install this operator
- [kubectl](https://kubernetes.io/docs/tasks/tools/) to view Kubernetes resources

## Create EKS Cluster
If you do not already have an EKS cluster, run the following to create one:

```bash
cd ../.. #go back to main directory to use MAKE commands

export AWS_CLUSTER_NAME=kaito-aws
export AWS_REGION=us-west-2
export AWS_PARTITION=aws
export AWS_K8S_VERSION=1.30
export KARPENTER_NAMESPACE=kube-system
export AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"

make deploy-aws-cloudformation
make create-eks-cluster
```

If you already have an EKS cluster, connect to it using
```bash
aws eks update-kubeconfig --name $CLUSTER_NAME --region $AWS_REGION
```

## Install Karpenter Controller
```bash
make aws-karpenter-helm
```

## Install Workspace Controller
```bash
make aws-patch-install-helm
```

## Verify installation
You can run the following commands to verify the installation of the controllers were successful.

Check status of the Helm chart installations.

```bash
helm list -n default
```

Check status of `workspace`.

```bash
kubectl describe deploy kaito-workspace -n kaito-workspace
```

Check status of `karpenter`.

```bash
kubectl describe deploy karpenter -n $KARPENTER_NAMESPACE
```

## Create a Workspace and start an inference service
Once the KAITO and Karpenter controllers are installed, you can follow these commands to start a falcon-7b inference service.

```bash
$ export kaito_workspace_aws="../../examples/inference/kaito_workspace_falcon_7b_aws.yaml"
$ cat $kaito_workspace_aws
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: aws-workspace
resource:
  instanceType: "g5.4xlarge"
  labelSelector:
    matchLabels:
      apps: falcon-7b
inference:
  preset:
    name: "falcon-7b"

$ kubectl apply -f $kaito_workspace_aws
```

The workspace status can be tracked by running the following command. When the `WORKSPACESUCCEEDED` column becomes `True`, the model has been deployed successfully.

```sh
$ kubectl get workspace workspace-falcon-7b
NAME                  INSTANCE            RESOURCEREADY   INFERENCEREADY    JOBSTARTED  WORKSPACESUCCEEDED  AGE
aws-workspace         g5.4xlarge          True            True              True        True                10m
```

Next, one can find the inference service's cluster ip and use a temporal `curl` pod to test the service endpoint in the cluster.

```sh
$ kubectl get svc aws-workspace
NAME                  TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)            AGE
aws-workspace         ClusterIP   <CLUSTERIP>  <none>        80/TCP,29500/TCP   10m

export CLUSTERIP=$(kubectl get svc aws-workspace -o jsonpath="{.spec.clusterIPs[0]}")
$ kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/chat -H "accept: application/json" -H "Content-Type: application/json" -d "{\"prompt\":\"YOUR QUESTION HERE\"}"
```