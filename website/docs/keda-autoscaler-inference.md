---
title: KEDA Auto-Scaler for inference workloads
---

- Feature status: Alpha

## Overview

This document outlines the steps to enable intelligent autoscaling for KAITO inference workloads by utilizing the following components and features:
 - [KEDA](https://github.com/kedacore/keda)
   - Kubernetes-based Event Driven Autoscaling component
 - [keda-kaito-scaler](https://github.com/kaito-project/keda-kaito-scaler)
   - A dedicated KEDA external scaler, eliminating the need for external dependencies such as Prometheus.
 - KAITO `InferenceSet` CRD and Controller
   - This new CRD and Controller were built on top of the KAITO workspace for intelligent autoscaling, introduced as an alpha feature in KAITO version `v0.8.0`

## Prerequisites
 - install KEDA
> The following example demonstrates how to install KEDA using Helm chart. For instructions on installing KEDA through other methods, please refer to the guide [here](https://github.com/kedacore/keda#deploying-keda).
```bash
helm repo add kedacore https://kedacore.github.io/charts
helm install keda kedacore/keda --namespace keda --create-namespace
```

 - install keda-kaito-scaler
```bash
helm repo add keda-kaito-scaler https://kaito-project.github.io/keda-kaito-scaler/charts/kaito-project
helm upgrade --install keda-kaito-scaler -n kaito-workspace keda-kaito-scaler/keda-kaito-scaler --create-namespace
```

## Enable this feature

This feature is available starting from KAITO v0.8.0, and the InferenceSet Controller must be enabled during the KAITO installation.

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.enableInferenceSetController=true \
  --wait
```

## Quickstart

### Create a Kaito InferenceSet for running inference workloads
 - The following example creates an InferenceSet for the phi-4-mini model, using annotations with the prefix `scaledobject.kaito.sh/` to supply parameter inputs for the KEDA Kaito Scaler:
   - `scaledobject.kaito.sh/auto-provision`
     - required, specifies whether KEDA Kaito Scaler will automatically provision a ScaledObject based on the `InferenceSet` object
   - `scaledobject.kaito.sh/metricName`
     - optional, specifies the metric name collected from the vLLM pod, which is used for monitoring and triggering the scaling operation, default is `vllm:num_requests_waiting`
   - `scaledobject.kaito.sh/threshold`
     - required, specifies the threshold for the monitored metric that triggers the scaling operation

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/metricName: "vllm:num_requests_waiting"
    scaledobject.kaito.sh/threshold: "10"
  name: phi-4
  namespace: default
spec:
  labelSelector:
    matchLabels:
      apps: phi-4
  replicas: 1
  nodeCountLimit: 5
  template:
    inference:
      preset:
        accessMode: public
        name: phi-4-mini-instruct
    resource:
      instanceType: Standard_NC24ads_A100_v4
EOF
```
 - In just a few seconds, the KEDA Kaito Scaler will automatically create the `scaledobject` and `hpa` objects. After a few minutes, once the inference pod is running, the KEDA Kaito Scaler will begin scraping metric values from the inference pod, and the status of the `scaledobject` and `hpa` objects will be marked as ready.

```bash
# kubectl get scaledobject
NAME           SCALETARGETKIND                  SCALETARGETNAME   MIN   MAX   READY   ACTIVE    FALLBACK   PAUSED   TRIGGERS   AUTHENTICATIONS           AGE
phi-4          kaito.sh/v1alpha1.InferenceSet   phi-4             1     5     True    True     False      False    external   keda-kaito-scaler-creds   10m

# kubectl get hpa
NAME                    REFERENCE                   TARGETS      MINPODS   MAXPODS   REPLICAS   AGE
keda-hpa-phi-4          InferenceSet/phi-4          0/10 (avg)   1         5         1          11m
```

That's it! Your KAITO workloads will now automatically scale based on the number of waiting inference requests(`vllm:num_requests_waiting`).
