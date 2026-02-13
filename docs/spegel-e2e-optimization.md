# Accelerating Kaito E2E Tests with Spegel P2P Distribution

Kaito E2E tests are bottlenecked by slow image pulls of the large `kaito-base` image. With a 3.56 GB base image that needs to be pulled during testing, developers face:

- **Long e2e test runtimes**: ~1 hour
- **CI/CD pipeline delays**: Test pipelines waiting for image downloads
- **Developer productivity loss**: Hours wasted waiting for tests to complete

**The core issue**: Every E2E test requires pulling the `kaito-base` image from Microsoft Container Registry (MCR), and with each pull taking 40+ seconds (sometimes > 3 minutes), test suites become very slow.

## Spegel P2P Image Caching

Spegel solves this by enabling nodes to share the `kaito-base` image with each other **within the cluster**. After the first node pulls the image from MCR, all subsequent pulls happen via fast peer-to-peer transfer instead of slow external registry downloads.

This document demonstrates E2E test performance improvements achieved by testing Spegel for pulling images on Azure Kubernetes Service (AKS).

---

## E2E Test Performance Improvements

### Primary Goal: Faster Kaito E2E Tests

The main objective of integrating Spegel is to **accelerate Kaito E2E test execution** by caching the `kaito-base` image across cluster nodes.


## How Spegel Works for Kaito E2E Tests

### The Workflow

1. **First Test Run** (Baseline):
   - Node A pulls `kaito-base:0.0.8` from MCR (41.6 seconds)
   - Spegel registers image layers in P2P network
   
2. **Subsequent Test Runs** (P2P Accelerated):
   - Node B needs `kaito-base:0.0.8` for test
   - Spegel serves image from Node A via P2P (35.7 seconds)
   - **5.9 seconds saved per pull** (14.1% faster)

3. **Parallel Test Execution**:
   - Multiple nodes pull simultaneously from multiple peers
   - No registry bottleneck or rate limiting

### Why This Matters for E2E Tests

- **Frequent Pulls**: E2E tests create/destroy pods repeatedly, each needing the base image
- **Multi-Node Tests**: Tests spread across nodes, all needing the same image
- **Parallel Execution**: Multiple test scenarios running concurrently

With Spegel, after the first pull, every subsequent test benefits from local P2P caching.



## Performance Benchmarks

After conducting comprehensive performance testing on AKS focusing on the `kaito-base` image used in E2E tests, plus a representative large image (PyTorch) to demonstrate scaling benefits.

### Test Environment

- **Platform**: Azure Kubernetes Service (AKS)
- **Cluster Configuration**: 3-node single nodepool
- **Node Size**: Standard_D4s_v3 (4 vCPUs, 16 GiB RAM)
- **Spegel Version**: v0.4.0 (DaemonSet deployment)

### Test Methodology

**Baseline Tests (Without Spegel):**
- Clean image removal from all nodes
- Fresh pull directly from external registry (MCR/Docker Hub)
- Timing measured via `kubectl describe pod` events

**P2P Tests (With Spegel):**
- Initial image seeded on Node A
- Test pull on Node B utilizing P2P distribution
- Spegel logs analyzed for P2P handler verification
- Multi-node setup ensuring authentic P2P testing


## Test Results

### Test 1: Kaito Base Image (Primary E2E Test Target)

**Image Details:**
- **Name**: `mcr.microsoft.com/oss/kubernetes/kaito/kaito-base:0.0.8`
- **Size**: 3.56 GB
- **Registry**: Microsoft Container Registry (MCR)
- **Usage**: Core base image for all Kaito E2E tests

**Performance Results:**

| Metric | Baseline (Direct Pull) | With Spegel (P2P) | Improvement |
|--------|----------------------|-------------------|-------------|
| **Pull Duration** | 41.6 seconds | 35.7 seconds | **14.1% faster** |
| **Time Saved** | - | 5.9 seconds | - |
| **Network Path** | Internet → MCR | Intra-cluster P2P | Local transfer |

**P2P Verification:**
- ✅ Confirmed `"handler":"mirror"` entries in Spegel logs
- ✅ Complete manifest and blob distribution via P2P
- ✅ Zero fallback to external registry
- ✅ All requests served from peer node

### Test 2: PyTorch Framework (Scaling Validation)

**Image Details:**
- **Name**: `pytorch/pytorch:2.0.1-cuda11.7-cudnn8-runtime`
- **Size**: 8.85 GB uncompressed
- **Registry**: Docker Hub

**Performance Results:**

| Metric | Baseline (Direct Pull) | With Spegel (P2P) | Improvement |
|--------|----------------------|-------------------|-------------|
| **Pull Duration** | 68.8 seconds | 43.2 seconds | **37.2% faster** |
| **Time Saved** | - | 25.6 seconds | - |
| **Network Path** | Internet → Docker Hub | Intra-cluster P2P | Local transfer |

**P2P Verification:**
- ✅ 8 confirmed mirror handler requests in Spegel logs
- ✅ Complete image distribution via P2P (manifest + all layers)
- ✅ Zero blob handler requests (no non-P2P fallback)
- ✅ Consistent P2P source throughout transfer

---

## Technical Deep Dive

1. **Repeated Image Pulls**: E2E tests create/destroy pods frequently, each requiring the base image
2. **Multi-Node Distribution**: Tests run across different nodes, all needing the same image
3. **Parallel Test Execution**: CI/CD runs multiple test scenarios simultaneously
4. **Large Base Image**: 3.56 GB kaito-base takes significant time to pull repeatedly
5. **Registry Rate Limits**: Frequent pulls hit MCR throttling limits

### Network Architecture Impact

- **Without Spegel**: Node → Internet Gateway → Azure CDN → MCR (multiple hops, variable latency)
- **With Spegel**: Node → Peer Node (single intra-cluster hop, ~1ms latency)

For E2E tests running in the same cluster, intra-cluster P2P is dramatically faster than repeated external registry pulls.

## Installation Guide for Spegel Test

### Prerequisites

- Kubernetes cluster
- `kubectl` configured to access your cluster
- Helm 3.x installed

### Step 1: Install Spegel

Install Spegel using Helm:

```bash
# Add Spegel Helm repository
helm repo add spegel https://spegel-org.github.io/helm-charts
helm repo update

# Install Spegel
helm install spegel spegel/spegel \
  --namespace spegel \
  --create-namespace \
  --set service.registry.port=5000
```

### Step 2: Verify Installation

Check that Spegel pods are running on all nodes:

```bash
# Verify DaemonSet deployment
kubectl get daemonset -n spegel

# Expected output:
# NAME     DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE
# spegel   3         3         3       3            3

# Check pod status
kubectl get pods -n spegel -o wide
```

### Step 3: Configure for Kaito E2E Tests

Spegel automatically intercepts image pull requests via containerd registry mirror configuration.

The `kaito-base` image will automatically be served via P2P after the first pull.

### Step 4: Verify P2P Distribution with Kaito Base Image

Test that Spegel is caching the kaito-base image:

```bash
# Pull kaito-base on first node (seeds P2P network)
kubectl run test-seed --image=mcr.microsoft.com/oss/kubernetes/kaito/kaito-base:0.0.8 \
  --restart=Never --command -- sleep 30

# Wait for completion
kubectl wait --for=condition=complete pod/test-seed --timeout=120s

# Now pull on a different node (should use P2P)
kubectl run test-p2p --image=mcr.microsoft.com/oss/kubernetes/kaito/kaito-base:0.0.8 \
  --restart=Never --command -- sleep 30

# Check pull time in events (should be ~35s instead of ~41s)
kubectl describe pod test-p2p | grep "Successfully pulled"
```

### Step 5: Monitor Spegel During E2E Tests

Monitor Spegel logs to confirm P2P activity:

```bash
# Watch Spegel logs for mirror handler activity
kubectl logs -n spegel -l app.kubernetes.io/name=spegel -f | grep "mirror"

# You should see entries like:
# {"level":"INFO","handler":"mirror","registry":"mcr.microsoft.com","status":200}
```

## Conclusion

**Problem**: Kaito E2E tests are slow due to repeated pulls of the 3.56 GB `kaito-base` image from external registries.

**Solution**: Spegel P2P caching enables nodes to share the kaito-base image within the cluster.


### Additional Resources

- [Spegel GitHub Repository](https://github.com/spegel-org/spegel)
- [Spegel Documentation](https://spegel.dev)
---

*Last Updated: November 2025*  
