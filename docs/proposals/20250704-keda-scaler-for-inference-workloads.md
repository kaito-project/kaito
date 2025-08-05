---
title: Keda Scaler For Inference Workloads In Kaito
authors:
  - "@rambohe-ch"
reviewers:
  - "@Fei-Guo"
  - "@helayoty"
  - "@zhuangqh"
creation-date: 2025-07-02
last-updated: 2025-07-04
status: provisional
see-also:
---

# Title

Keda Scaler for inference workloads in Kaito

## Summary

As the number of waiting inference requests increases, it is necessary to scale more inference instances in order to prevent blocking inference requests. On the other hand, if the number of waiting inference requests declines, we should consider reducing inference instances to improve GPU resource utilization.

In this proposal, we hope to enhance the keda system and develop a customized scaler which is specialized for scaling GPU workloads for Kaito. This customized scaler is designed for a minimalistic configuration experience which allows users to easily get started without requiring specialized knowledge of LLM.

## Motivation

LLM inference service is a basic and widely-used feature in Kaito, and Kaito community interest in auto-scaler for inference workloads continues to intensify. Related issues: [#306](https://github.com/kaito-project/kaito/issues/306), [#1104](https://github.com/kaito-project/kaito/issues/1104).

From the technical perspective, we don't want to make a new wheel for auto-scaler, so it's a good idea to expand a customized scaler for keda to dynamically adjusts the number of inference instances based on request volume—scaling up during traffic spikes to improve inference speed, and scaling down during low demand to minimize GPU resource waste. Furthermore, for workloads with predictable, recurring traffic patterns, cron scaler in keda can proactively adjust capacity, ensuring resources are ready before they are needed.

The auto-scaler solution for Kaito should be shown as follows:

![keda-kaito-scaler](../../static/img/keda-kaito-scaler.png)

We will divide this auto-scaler feature into two parts as follows:

- Part one: support scale subresource API for workspace, so different auto-scaler solutions such as KEDA, HPA, etc. can be integrated with Kaito to manage inference workloads dynamically. This part is addressed in another proposal: https://github.com/kaito-project/kaito/pull/1184.
- Part two: support a customized scaler(named kaito-scaler) of keda. The kaito scaler is designed for a minimalistic configuration experience, with most parameters pre-tuned for optimal performance. This allows users to easily get started without requiring specialized knowledge of LLM. This part will be addressed in this proposal. This scaler supports reactive (metric-based) scaling.

To ensure ease of use, the specialized kaito scaler is hosted in an independent repo (kaito-project/keda-kaito-scaler). At the same time, the keda-kaito-scaler component can work with Kaito and keda without depending on any other third-party components.

### Goals

- keda-kaito-scaler is a specialized scaler plugin for kada which is specialized for scaling gpu workloads automatically, and can integrate with kaito to work.
- compared to native prometheus scaler in keda, keda-kaito-scaler can provides the same capability for scaling gpu workloads and with user-friendly and minimalistic configurations.

### Non-Goals/Future Work

- The time efficiency of the auto-scaler is not within the scope of this proposal, as it is influenced by mutliple external factors, including GPU node provisioning, LLM image pulling, etc.
- Only support scale vllm workload, and non-vllm is not covered.

## Proposal

Keda can provides both metric-based and time-based scaler capability, but for metric-based scaler it should work together with prometheus. The detailed auto-scaler architecture is shown in the following figure:

![keda-prometheus-cron-auto-scaler](../../static/img/keda-prometheus-cron-auto-scaler.png)

- **Keda**: includes two components: metrics-adapter and keda-core. metrics-adpter is used for exposing external metrics on kube-apiserver wihch will be used by native HPA. keda-core includes the core logic of keda system, like the generation of HPA resource according to ScaledObject, and supporting different scalers(like prometheus, cron scalers).
- **Scalers**: Keda providers more than 100+ scalers, but these scalers are only used for providing metrics for native HPA, and the scaling logic and actions are taken by native HPA.
- **HPA**: native scaler capability in K8s, scale workloads accroding to HPA resource that created by keda, pull metrics and calculate the desired replicas at a interval time and invokes the `/scale` subresource API of the target workspace when scaling needed.
- **Prometheus**: scrape specified metrics at a interval cycle accroding to PodMonitor configurations.

### Time-based Scaler of Keda

reference link: https://keda.sh/docs/2.17/scalers/cron/

The KEDA cron scaler allows you to scale workloads based on time schedules, which is particularly useful for workloads with predictable traffic patterns. This is ideal for scenarios where you know peak hours in advance and want to proactively scale resources before demand increases.

#### Example: Business Hours Scaling

Below is an example of a `ScaledObject` that scales a Kaito Workspace based on business hours:
- **Scale up to 10 replicas** from 6AM to 8PM (peak hours)
- **Scale down to 1 replica** otherwise (off-peak hours)

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: kaito-workspace-business-hours-scaler
  namespace: kaito-workloads
spec:
  # Target Kaito Workspace to scale
  scaleTargetRef:
    apiVersion: kaito.sh/v1alpha1
    kind: Workspace
    name: my-vllm-workspace

  # Scaling boundaries
  minReplicas: 1
  maxReplicas: 10

  # Cron-based triggers for time-based scaling
  triggers:
  # Scale up to 10 replicas at 6AM (start of business hours)
  - type: cron
    metadata:
      timezone: "America/New_York"  # Adjust timezone as needed
      start: "0 6 * * 1-5"         # 6AM Monday to Friday
      end: "0 20 * * 1-5"          # 8PM Monday to Friday
      desiredReplicas: "10"        # Scale to 10 replicas during business hours

  # Scale down to 1 replica at 8PM (end of business hours)
  - type: cron
    metadata:
      timezone: "America/New_York"  # Adjust timezone as needed
      start: "0 20 * * 1-5"        # 8PM Monday to Friday
      end: "0 6 * * 1-5"           # 6AM Monday to Friday (next day)
      desiredReplicas: "1"         # Scale to 1 replica during off-hours
```

#### Cron Expression Format

The cron expressions follow the standard format:
```
* * * * *
│ │ │ │ │
│ │ │ │ └─ day of week (0-6, Sunday=0)
│ │ │ └─── month (1-12)
│ │ └───── day of month (1-31)
│ └─────── hour (0-23)
└───────── minute (0-59)
```

### Metric-based Scaler of Keda

reference link: https://keda.sh/docs/2.17/scalers/prometheus/

#### Prometheus configuration

When using the [Prometheus Operator](https://prometheus-operator.dev/), the recommended way to configure scraping is to create a `PodMonitor` custom resource. This resource declaratively defines how a set of pods should be monitored, and the Prometheus Operator will automatically generate and update the required Prometheus scrape configurations.

Below is an example of a `PodMonitor` resource designed to discover and scrape metrics from all vLLM inference pods every 15 seconds.

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: kaito-vllm-inference-monitor
  # This PodMonitor should be created in the same namespace as your Prometheus instance,
  # or in a namespace that Prometheus is configured to watch.
  namespace: monitoring
  labels:
    # This label allows the Prometheus custom resource to discover this PodMonitor.
    # The key/value may vary depending on your Prometheus Operator setup.
    release: prometheus
spec:
  # Use namespaceSelector to specify which namespaces to look for pods in.
  # If your vLLM pods are in a specific namespace, list it here.
  namespaceSelector:
    matchNames:
      - kaito-workloads # <-- Change this to the namespace of your inference pods.

  # Use a label selector to identify the target pods to scrape.
  selector:
    matchLabels:
      # This label must be present on your vLLM pods.
      app.kubernetes.io/component: inference
      app.kubernetes.io/part-of: kaito

  # podMetricsEndpoints defines the port and path to scrape.
  podMetricsEndpoints:
  - port: metrics
    # This must match the name of the port in the pod's container spec (e.g., `ports.name`).
    # For vLLM, the container port (e.g., 5000 or 8000) should be named 'metrics'.

    # Override the default scrape interval.
    interval: 15s

    # The path to the metrics endpoint.
    path: /metrics
```

To make this `PodMonitor` work, we must ensure your vLLM pods are created with the correct metadata:

1.  **Labels**: The pods must have labels that match the `selector.matchLabels` in the `PodMonitor`. For example:
    ```yaml
    metadata:
      labels:
        app.kubernetes.io/component: inference
        app.kubernetes.io/part-of: kaito
    ```

2.  **Named Container Port**: The container that exposes the metrics endpoint must have a named port that matches the `port` field in the `podMetricsEndpoints` section.
    ```yaml
    spec:
      containers:
      - name: vllm-inference
        ports:
        - containerPort: 5000
          name: metrics # <-- This name must match the PodMonitor's endpoint port.
    ```

By using a `PodMonitor`, you can manage the entire lifecycle of your Prometheus scrape configuration through the Kubernetes API, which is more scalable and robust than manually editing configuration files.

#### ScaledObject of Keda

Below is a complete example of a KEDA `ScaledObject` that demonstrates how to autoscale a Kaito `Workspace` based on the `vllm:num_requests_waiting` metric from Prometheus. This configuration is specifically optimized for GPU workloads and includes the following key features:

- **Prometheus Integration**: Connects to Prometheus with TLS authentication to query vLLM waiting request metrics
- **Conservative Scaling**: Implements slow, deliberate scaling policies (1 pod every 5 minutes for scale-up, 1 pod every 10 minutes for scale-down) to account for slow GPU node provisioning
- **Dead Zone Configuration**: Uses tolerance settings to create distinct high (11) and low (5) thresholds, preventing flapping when metrics fluctuate between these values
- **Anti-Flapping Protection**: Combines stabilization windows with conservative scaling policies to ensure service stability
- **No Scale-to-Zero**: Maintains a minimum of 1 replica to ensure continuous service availability

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: kaito-vllm-workspace-scaler
  # This ScaledObject should be in the same namespace as the Workspace it scales.
  namespace: kaito-workloads
spec:
  # scaleTargetRef points to the Kaito Workspace resource to be scaled.
  scaleTargetRef:
    apiVersion: kaito.sh/v1alpha1 # Or v1beta1, depending on your CRD version
    kind: Workspace
    name: my-vllm-workspace # <-- Change this to the name of your Workspace

  # Minimum and maximum number of replicas.
  # minReplicas: 1 prevents KEDA from scaling down to zero.
  minReplicas: 1
  maxReplicas: 10

  # Trigger configuration for Prometheus.
  triggers:
    - type: prometheus
      metadata:
        # The address of the Prometheus server. Use https for TLS.
        serverAddress: https://prometheus.monitoring.svc:9090
        # The PromQL query to get the average number of waiting requests per pod.
        # This query calculates the average metric across all pods belonging to the component.
        # Ensure your pods have the 'app.kubernetes.io/component: inference' label.
        query: |
          avg(vllm:num_requests_waiting{app.kubernetes.io/component="inference"})
        # The target value for the metric. HPA will aim to keep the metric at this value.
        # We set this to 10 to create asymmetric scaling thresholds.
        threshold: "10"
        # Specifies that TLS authentication is required to connect to Prometheus.
        authModes: "tls"
      # References a TriggerAuthentication resource containing the TLS secrets.
      authenticationRef:
        name: keda-prometheus-tls

  # Advanced HPA configuration for fine-grained scaling behavior.
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        # Scale-up behavior: configured to be conservative due to slow GPU provisioning.
        scaleUp:
          # A short stabilization window allows for quick reactions to load increases.
          stabilizationWindowSeconds: 30
          # Tolerance for scale-up: triggers when metric > threshold * (1 + tolerance)
          # With threshold: 10 and tolerance: 0.1, scale-up triggers at 11
          tolerance: 0.1
          policies:
            # Only allow scaling up by 1 pod every 5 minutes to ensure stability
            # during slow GPU node provisioning.
            - type: Pods
              value: 1
              periodSeconds: 300 # 5 minutes
          # 'Max' selects the policy that allows the fastest scaling.
          selectPolicy: Max

        # Scale-down behavior: configured to be very conservative to avoid flapping.
        scaleDown:
          # A long stabilization window ensures the load is consistently low before scaling down.
          stabilizationWindowSeconds: 300
          # Tolerance for scale-down: triggers when metric < threshold * (1 - tolerance)
          # With threshold: 10 and tolerance: 0.5, scale-down triggers at 5
          tolerance: 0.5
          policies:
            # Only allow scaling down by 1 pod every 10 minutes to maintain
            # service availability and prevent aggressive downscaling.
            - type: Pods
              value: 1
              periodSeconds: 600 # 10 minutes
          selectPolicy: Max
```

By setting the `threshold` to `10` and using asymmetric tolerance values, we create more responsive scaling boundaries:
- **Scale-Up Threshold**: Scaling up will be triggered when the metric value exceeds `11` (`10 * (1 + 0.1)`).
- **Scale-Down Threshold**: Scaling down will be triggered when the metric value drops below `5` (`10 * (1 - 0.5)`).

This configuration creates a dead zone between 5 and 11, with more aggressive scale-up behavior (shorter threshold gap) and more conservative scale-down behavior (wider threshold gap). This approach prevents flapping while ensuring quick response to load increases and stability during load decreases.

#### TLS Authentication for Prometheus

To use TLS authentication, you must create a `TriggerAuthentication` resource and a corresponding Kubernetes `Secret` that holds the client certificate, private key, and CA certificate.

1.  **Create the Secret**:
    First, create a secret containing your TLS assets. The keys (`tls.crt`, `tls.key`, `ca.crt`) must match the parameters in the `TriggerAuthentication`.

    ```bash
    kubectl create secret generic prometheus-client-secret \
      --from-file=tls.crt=/path/to/client.crt \
      --from-file=tls.key=/path/to/client.key \
      --from-file=ca.crt=/path/to/ca.crt \
      -n keda # <-- Create the secret in the KEDA namespace
    ```

2.  **Create the TriggerAuthentication**:
    This resource tells KEDA how to use the secret to authenticate with Prometheus.

    ```yaml
    apiVersion: keda.sh/v1alpha1
    kind: TriggerAuthentication
    metadata:
      name: keda-prometheus-tls
      # This resource must be in the same namespace as KEDA itself.
      namespace: keda
    spec:
      secretTargetRef:
        - parameter: tls
          name: prometheus-client-secret
          key: tls.crt
        - parameter: key
          name: prometheus-client-secret
          key: tls.key
        - parameter: ca
          name: prometheus-client-secret
          key: ca.crt
    ```

By applying these resources, you establish a secure, robust, and efficient autoscaling system for your Kaito inference workloads.

#### Challenges of this solution

While the KEDA + Prometheus approach provides powerful autoscaling capabilities for vllm workloads, it presents several significant challenges for end users:

**1. Complex Setup and Configuration**
- **Multi-component architecture**: Users must deploy and configure Prometheus Operator, KEDA, and multiple custom resources (`PodMonitor`, `ScaledObject`, `TriggerAuthentication`, `Secret`)
- **Configuration complexity**: Each component requires detailed configuration with specific labels, selectors, and parameters that must align correctly

**2. Monitoring Stack Dependency**
- **Prometheus requirement**: Users must deploy and maintain a full Prometheus monitoring stack, which adds operational overhead and resource consumption
- **Operator knowledge**: Requires understanding of Prometheus Operator concepts like `ServiceMonitor`, `PodMonitor`, and `PrometheusRule`
- **Storage and retention**: Prometheus requires persistent storage and proper retention policies, adding to infrastructure costs

**3. Security Configuration Complexity**
- **TLS certificate management**: Setting up TLS authentication requires generating, distributing, and rotating client certificates
- **Secret management**: Multiple secrets must be created and maintained across different namespaces

**4. Domain Knowledge Requirements**
- **Prometheus expertise**: Users need to understand PromQL, metric types, and Prometheus configuration
- **Kubernetes scaling knowledge**: Deep understanding of HPA behavior, scaling policies, and stabilization windows is required
- **LLM-specific tuning**: Users must understand vLLM metrics and how they relate to inference performance

These challenges highlight the need for a more user-friendly, domain-specific solution that abstracts away the complexity while providing the same autoscaling capabilities. This is the primary motivation for developing the specialized `keda-kaito-scaler` plugin proposed in this document.

### Keda-Kaito-Scaler Proposal

In order to address the above challenges, we consider to create a new component named keda-kaito-scaler to provide features as follows:
- kaito scaler: works as an external scaler of keda, and collects metrics from inference pods according to ScaledObject configurations. This means that `PodMonitor`, `TriggerAuthentication`, `Secret` configurations aren't needed, and users only need to configure `ScaledObject`. at the same time, users don't need to maintain the Prometheus stack.
- kaito scaler manager: works as a deployment, includes webhooks for configuring default values for `ScaledObject`, so users don't need to dive into the details of HPA behavior or vllm metrics. and controllers for ensuring secret which used by grpc connection between keda core and kaito scaler(external scaler).

![keda-kaito-scaler-arch](../../static/img/keda-kaito-scaler-arch.png)

#### Trigger Specification of Kaito Scaler

The Kaito external scaler provides a simplified configuration interface for scaling vLLM inference workloads. Unlike the Prometheus scaler, it directly scrapes metrics from inference pods, eliminating the need for a separate monitoring stack.

```yaml
triggers:
- type: external
  metadata:
    # Required fields

    # Unique identifier for this scaler instance
    scalerName: keda-kaito-scaler

    # threshold for scaling up/down
    # When average waiting requests per pod exceeds this value * (1 + scaleUp.tolerance), scale up
    # When average waiting requests per pod drops below this value * (1 - scaleDown.tolerance), scale down
    threshold: "10"

    # Optional fields:

    # The name of the workspace resource
    # Default: name of the ScaledObject.Spec.scaleTargetRef
    workspaceName: my-vllm-workspace

    # The namespace of the workspace resource
    # Default: namespace of the ScaledObject object.
    workspaceNamespace: kaito-workloads

    # The address of the external scaler
    scalerAddress: kaito-scaler.keda.svc.cluster.local:9090

    # Metric name to scrape from pods
    # Default: "vllm:num_requests_waiting"
    metricName: "vllm:num_requests_waiting"

    # Protocol for scraping metrics from pods
    # Default: "https"
    metricPorotocol: "https"

    # Port name for scraping metrics from pods
    # Default: "5000"
    metricPort: "5000"

    # Path for metrics endpoint on pods
    # Default: "/metrics"
    metricPath: "/metrics"

    # Optional: Timeout for metric scraping in seconds
    # Default: 5
    scrapeTimeout: "5"
  # Optional: TLS Authentication used by keda-core to acess keda-kaito-scaler
  # Default: "keda-kaito-creds"
  authenticationRef:
    name: keda-kaito-creds
    kind: ClusterTriggerAuthentication
```

**Example ScaledObject with Kaito External Scaler**

Here's a complete example showing how to use the Kaito external scaler:

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: kaito-vllm-workspace-scaler
  namespace: kaito-workloads
spec:
  # Target Kaito Workspace to scale
  scaleTargetRef:
    apiVersion: kaito.sh/v1alpha1
    kind: Workspace
    name: my-vllm-workspace

  # Scaling boundaries
  minReplicas: 1
  maxReplicas: 10

  # Simplified trigger configuration - no Prometheus required!
  triggers:
  - type: external
    metadata:
      scalerName: keda-kaito-scaler
      metricName: "vllm:num_requests_waiting"
      threshold: "10"
      # All other settings use sensible defaults

  # Optional: Advanced HPA behavior (can be omitted for defaults)
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 30
          tolerance: 0.1
          policies:
          - type: Pods
            value: 1
            periodSeconds: 300 # 5 minutes
        scaleDown:
          stabilizationWindowSeconds: 300
          tolerance: 0.5
          policies:
          - type: Pods
            value: 1
            periodSeconds: 600 # 10 minutes
```

#### The Implementation of keda-kaito-scaler

The keda-kaito-scaler consists of two main components: an external scaler that implements the KEDA external scaler interface, and a manager that handles Kubernetes resources and lifecycle management.

##### External Scaler Interface Design

The detailed interface for external scaler of keda: https://keda.sh/docs/2.17/concepts/external-scalers/#external-scaler-grpc-interface

```go
// External Scaler GRPC Service Interface
// This implements the KEDA external scaler protocol
type ExternalScaler interface {
    // IsActive determines if the scaler should be active for a given ScaledObject
    IsActive(ctx context.Context, req *pb.ScaledObjectRef) (*pb.IsActiveResponse, error)

    // GetMetricSpec returns the metric specification for HPA
    GetMetricSpec(ctx context.Context, req *pb.ScaledObjectRef) (*pb.GetMetricSpecResponse, error)

    // GetMetrics returns the current metric values
    GetMetrics(ctx context.Context, req *pb.GetMetricsRequest) (*pb.GetMetricsResponse, error)
}

// Kaito Scaler Implementation
type KaitoScaler struct {
    kubeClient    client.Client
    httpClient    *http.Client
}

// IsActive checks if the target Kaito Workspace is ready for scaling
func (k *KaitoScaler) IsActive(ctx context.Context, req *pb.ScaledObjectRef) (*pb.IsActiveResponse, error) {
    // Get the target Kaito Workspace
	var workspace kaito.Workspace
    err := k.Get(ctx, req.Namespace/Name, &workspace)
    if err != nil {
        return &pb.IsActiveResponse{Result: false}, nil
    }

    // Check if workspace is ready and has inference enabled
    isActive := workspace.Status.Phase == kaitov1alpha1.WorkspacePhaseReady &&
               workspace.Spec.Inference != nil &&
               workspace.Spec.Inference.Replicas > 0

    return &pb.IsActiveResponse{Result: isActive}, nil
}

// GetMetricSpec returns the metric specification for this scaler
func (k *KaitoScaler) GetMetricSpec(ctx context.Context, req *pb.ScaledObjectRef) (*pb.GetMetricSpecResponse, error) {
    return &pb.GetMetricSpecResponse{
        MetricSpecs: []*pb.MetricSpec{
            {
                MetricName:   fmt.Sprintf("s%d-%s", req.TriggerIndex, req.MetricName),
                TargetSize:   req.Threshold,
                MetricType:   pb.MetricType_AverageValue,
            },
        },
    }, nil
}

// GetMetrics retrieves current metrics from inference pods
func (k *KaitoScaler) GetMetrics(ctx context.Context, req *pb.GetMetricsRequest) (*pb.GetMetricsResponse, error) {
    // Parse scaler metadata from the request
    scalerConfig, err := k.parseScalerMetadata(req.ScaledObjectRef, req.MetricName)
    if err != nil {
        return nil, fmt.Errorf("failed to parse scaler metadata: %w", err)
    }

    // Get target Kaito Workspace
	var workspace kaito.Workspace
    err := k.Get(ctx, req.Namespace/Name, &workspace)
    if err != nil {
        return &pb.IsActiveResponse{Result: false}, nil
    }

    // Get inference pods for this workspace
    pods, err := k.getInferencePods(ctx, workspace)
    if err != nil {
        return nil, fmt.Errorf("failed to get inference pods: %w", err)
    }

    // Collect metrics from all pods with intelligent fallback for missing metrics
    var totalValue float64
    var podCount int
    var readyPodCount int
    var missingMetricCount int

    for _, pod := range pods {
        podCount++
        if k.isPodReady(pod) {
            readyPodCount++
            value, err := k.getMetricFromPod(ctx, pod, scalerConfig)
            if err != nil {
                // Pod is ready but metric is unavailable (e.g., endpoint not responding)
                missingMetricCount++
                continue
            }
            totalValue += float64(value)
        } else {
            // Pod is not ready (pending, starting, etc.)
            missingMetricCount++
        }
    }

    // Apply fallback for missing metrics to prevent flapping
    if missingMetricCount > 0 {
        fallbackValue := k.calculateFallbackValue(scalerConfig, totalValue, readyPodCount-missingMetricCount)
        totalValue += fallbackValue * float64(missingMetricCount)
    }

    // Calculate average metric value per pod (including fallback values)
    var avgValue float64
    if podCount > 0 {
        avgValue = totalValue / float64(podCount)
    }

    return &pb.GetMetricsResponse{
        MetricValues: []*pb.MetricValue{
            {
                MetricName:  req.MetricName,
                MetricValue: int64(avgValue),
            },
        },
    }, nil
}

// calculateFallbackValue determines the fallback metric value for missing pods
// to prevent scaling flapping based on the current scaling direction
func (k *KaitoScaler) calculateFallbackValue(config *ScalerConfig, currentTotal float64, validPodCount int) float64 {
    // Calculate current average from valid pods
    var currentAvg float64
    if validPodCount > 0 {
        currentAvg = currentTotal / float64(validPodCount)
    }

    // Determine scaling direction based on current average vs threshold
    threshold := config.Threshold
    if currentAvg > threshold {
        // Scale-up scenario: current load is high
        // Use 0 for missing pods to avoid overestimating load
        // This prevents unnecessary aggressive scale-up due to missing metrics
        return 0
    } else {
        // Scale-down scenario or steady state: current load is low/normal
        // Use high threshold value for missing pods to be conservative
        // This prevents premature scale-down when some pods are not reporting metrics
        return threshold * 1.5 // Use 1.5x threshold as "high" value to ensure conservative scaling
    }
}

type ScalerConfig struct {
    MetricName    string
    MetricsPort   string
    MetricsPath   string
    ScrapeTimeout time.Duration
    Threshold     float64
}

func (k *KaitoScaler) parseScalerMetadata(ref *pb.ScaledObjectRef, metricName string) (*ScalerConfig, error) {
    // Parse metadata from ScaledObject (passed via KEDA)
    // This would extract values like metricsPort, metricsPath, threshold, etc.
    config := &ScalerConfig{
        MetricName:    "vllm:num_requests_waiting", // default
        MetricsPort:   "5000",                      // default vLLM port
        MetricsPath:   "/metrics",                  // default
        ScrapeTimeout: 5 * time.Second,             // default
        Threshold:     10,                          // default threshold for scaling decisions
    }

    // Override with values from ScaledObject metadata if provided
    // Implementation would parse ref.ScalerMetadata map to extract:
    // - threshold: scaling threshold value
    // - metricPort: port for metrics scraping
    // - metricPath: path for metrics endpoint
    // - scrapeTimeout: timeout for metric collection

    return config, nil
}
```

##### Server TLS Configuration For Kaito Scaler

The Kaito external scaler implements a secure gRPC server that uses TLS certificates from the `keda-kaito-scaler-certs` secret for encrypted communication with KEDA core.

The external scaler gRPC server must be configured to use the server certificates from the `keda-kaito-scaler-certs` secret. Since the secret is created by the same pod, we can't mount it as a volume. Instead, we use a Kubernetes client with a secret lister in the `GetCertificate` callback to dynamically retrieve certificates:

```go
// CertificateManager manages TLS certificates using Kubernetes informers for efficient caching
type CertificateManager struct {
    secretLister    corelisters.SecretLister
    secretNamespace string
}

// getSecret retrieves the certificate secret using the informer lister (cached)
func (cm *CertificateManager) getSecret() (*corev1.Secret, error) {
    secret, err := cm.secretLister.Secrets(cm.secretNamespace).Get(cm."keda-kaito-scaler-certs")
    if err != nil {
        return nil, fmt.Errorf("failed to get certificate secret from cache: %w", err)
    }
    return secret, nil
}

// GetServerCertificate retrieves server certificate using cached informer data
func (cm *CertificateManager) GetServerCertificate() (*tls.Certificate, error) {
    secret, err := cm.getSecret()
    if err != nil {
        return nil, err
    }

    // Extract server certificate and key
    serverCertPEM, exists := secret.Data["server.crt"]
    if !exists {
        return nil, fmt.Errorf("server.crt not found in secret")
    }

    serverKeyPEM, exists := secret.Data["server.key"]
    if !exists {
        return nil, fmt.Errorf("server.key not found in secret")
    }

    // Parse the certificate and key
    cert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
    if err != nil {
        return nil, fmt.Errorf("failed to parse server certificate: %w", err)
    }

    return &cert, nil
}

// GetCAPool retrieves CA certificate pool using cached informer data
func (cm *CertificateManager) GetCAPool() (*x509.CertPool, error) {
    secret, err := cm.getSecret()
    if err != nil {
        return nil, err
    }

    // Extract CA certificate
    caCertPEM, exists := secret.Data["ca.crt"]
    if !exists {
        return nil, fmt.Errorf("ca.crt not found in secret")
    }

    // Create certificate pool and add CA
    caCertPool := x509.NewCertPool()
    if !caCertPool.AppendCertsFromPEM(caCertPEM) {
        return nil, fmt.Errorf("failed to parse CA certificate")
    }

    return caCertPool, nil
}

// TLS Configuration for gRPC Server with informer-based certificate retrieval
func setupTLSServer(certManager *CertificateManager) (*grpc.Server, error) {
    // Configure TLS with dynamic certificate loading using informer cache
    tlsConfig := &tls.Config{
        // GetCertificate dynamically loads server certificate from informer cache
        // This allows automatic pickup of renewed certificates without restart
        GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
            return certManager.GetServerCertificate()
        },
        GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
            // Dynamically load CA certificate for client verification
            caCertPool, err := certManager.GetCAPool()
            if err != nil {
                return nil, fmt.Errorf("failed to load CA certificate: %w", err)
            }

            return &tls.Config{
                ClientAuth: tls.RequireAndVerifyClientCert,
                ClientCAs:  caCertPool,
                MinVersion: tls.VersionTLS12,
            }, nil
        },
        MinVersion: tls.VersionTLS12,
    }

    // Create gRPC server with TLS credentials
    creds := credentials.NewTLS(tlsConfig)
    server := grpc.NewServer(grpc.Creds(creds))
    return server, nil
}
```

##### Scaler Manager

The Scaler Manager includes a **Scaler Webhook** that provides defaults for ScaledObject configurations targeting Kaito Workspaces. and a **Scaler Controller** for ensuring certificates for tls connection between keda-core and kaito scaler.

**Scaler Webhook**

The webhook serves as a mutating admission controller that automatically applies GPU-optimized defaults to ScaledObjects.

| Configuration | Value | Rationale |
|---------------|-------|-----------|
| `minReplicaCount: 1` | Never scale to zero | Ensures continuous inference availability; avoids cold start delays |
| `scaleUp.periodSeconds: 300` | 5 minutes | Accounts for slow GPU node provisioning and container startup |
| `scaleDown.periodSeconds: 600` | 10 minutes | Very conservative to prevent aggressive downscaling of expensive resources |
| `scaleUp.stabilizationWindowSeconds: 30` | 30 seconds | Quick response to load increases for better user experience |
| `scaleDown.stabilizationWindowSeconds: 300` | 5 minutes | Ensures load is consistently low before scaling down |
| `metricProtocol: "https"` | https | Standard vLLM metrics protocol |
| `metricPort: "5000"` | Port 5000 | Standard vLLM metrics port |
| `metricPath: "/metrics"` | /metrics | Standard vLLM metrics path |
| `scrapeTimeout: "5"` | 5 seconds | Reasonable timeout for metrics collection |
| `authenticationRef.name: "keda-kaito-creds"` | keda-kaito-creds | ClusterTriggerAuthentication used for TLS authentication between keda-core and keda-kaito-scaler across namespaces |
| `authenticationRef.kind: "ClusterTriggerAuthentication"` | ClusterTriggerAuthentication | all scaledObjects use the same credentials |

**Scaler Controller**

The Scaler Controller is responsible for managing TLS certificates and authentication resources required for secure GRPC communication between KEDA core and the external Kaito scaler. This controller ensures that certificates, secrets, and authentication resources are automatically generated, distributed, and renewed without manual intervention.

**Certificate Structure:**
- **CA Certificate**: Root certificate authority for the scaler communication
- **Server Certificate**: Used by the external Kaito scaler GRPC server
- **Client Certificate**: Used by KEDA core to authenticate with the external scaler
- **DNS SANs**: Includes all necessary service names and IPs for flexible deployment(like: scaler service FQDN: kaito-scaler.keda.svc.cluster.local)

**Managed Resources:**

1. **Secret Resource (`keda-kaito-scaler-certs`)**:
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: keda-kaito-scaler-certs
     namespace: keda
   type: kubernetes.io/tls
   data:
     ca.crt: <base64-encoded-ca-certificate>
     tls.crt: <base64-encoded-client-certificate>
     tls.key: <base64-encoded-client-private-key>
     server.crt: <base64-encoded-server-certificate>
     server.key: <base64-encoded-server-private-key>
   ```

2. **ClusterTriggerAuthentication Resource (`keda-kaito-creds`)**:
   ```yaml
   apiVersion: keda.sh/v1alpha1
   kind: ClusterTriggerAuthentication
   metadata:
     name: keda-kaito-creds
   spec:
     secretTargetRef:
       - parameter: caCert
         name: keda-kaito-scaler-certs
         key: ca.crt
       - parameter: tlsClientCert
         name: keda-kaito-scaler-certs
         key: tls.crt
       - parameter: tlsClientKey
         name: keda-kaito-scaler-certs
         key: tls.key
   ```

**Controller Responsibilities:**

- **Certificate Generation**: Automatically generates CA, server, and client certificates with appropriate DNS SANs
- **Secret Management**: Creates and maintains the `keda-kaito-scaler-certs` secret in the same namespace as KEDA core (typically `keda` namespace) with all necessary certificate data
- **ClusterTriggerAuthentication Management**: Ensures the `keda-kaito-creds` ClusterTriggerAuthentication resource exists and references the `keda-kaito-scaler-certs` secret
- **Certificate Renewal**: Monitors certificate expiration and automatically renews certificates before they expire
- **Cross-Namespace Coordination**: Manages resources in the KEDA namespace while providing cluster-wide authentication for all scaledObjects that are targeting Kaito workloads.

## Alternatives

This section compares three different auto-scaling solutions for LLM inference workloads in Kaito: KEDA + Prometheus, KEDA + Kaito Scaler, and LLMAutoScaler.

### Comparison Overview

| Aspect | KEDA + Prometheus | KEDA + Kaito Scaler(Proposed) | LLMAutoScaler |
|--------|-------------------|---------------------|---------------|
| **Complexity** | High | Low | Medium |
| **Setup Time** | Hours | Minutes | Minutes |
| **Dependencies** | Prometheus Stack + KEDA | KEDA Only | Custom Implementation |
| **Configuration Lines** | 100+ YAML lines | 10-15 YAML lines | 10-15 YAML lines |
| **Domain Knowledge Required** | PromQL, HPA, TLS, Monitoring | Basic KEDA concepts | Custom scaling logic |
| **Maintenance Overhead** | High | Low | Medium |
| **Production Readiness** | High (with effort) | High (out-of-box) | Depends on implementation |
| **Vendor Lock-in** | Low | Low | High (Kaito-specific) |

## Implementation History
 07/07/2025: Open proposal PR

