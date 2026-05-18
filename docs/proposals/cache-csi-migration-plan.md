# Cache Integration — CSI Driver Migration Plan

## Context

The initial distributed cache integration (proposal `20260518-distributed-cache-integration.md`,
branch `cache-integration`) uses an init-container-based approach to inject Tachyon libraries
into inference pods. The Tachyon team has since developed a CSI driver with a mutating webhook
that handles library injection automatically. This plan migrates the KAITO implementation to
use the CSI-driver-based approach.

### Key References

- **Tachyon CSI driver design**: `/home/simon/src/Tachyon` on branch `users/simon/csi` — `docs/design/csi-driver.md`
- **TachyonKV connector design**: `/home/simon/src/Tachyon` on branch `users/simon/kvcache` — `src/subsystems/storage/kv_client/docs/tachyonkv-kv-cache-design.md`
- **AI Runway integration design**: `/home/simon/src/airunway/docs/distributed-cache.md`
- **Current KAITO implementation**: branch `cache-integration` (this repo)

### Motivation

1. **Multi-provider reuse**: AI Runway orchestrates KAITO, Dynamo, llm-d, and KubeRay. A shared
   library injection mechanism (CSI webhook) serves all providers equally without each needing
   bespoke init container logic.

2. **Reduced KAITO responsibility**: KAITO no longer deploys Tachyon. It's a user-installed
   prerequisite (same pattern as Gateway API in AI Runway). KAITO detects and uses it.

3. **Cleaner architecture**: The CSI webhook handles ALL library injection (native .so AND
   Python packages) via a hostPath mount. No init containers, no image coupling.

4. **AI Runway compatibility**: AI Runway maps `modeldeployment.spec.cache.kvCache` →
   `workspace.cache.kvCache`. KAITO handles the rest natively. This keeps KAITO fully
   functional standalone while being automatable by AI Runway.

---

## Architecture Change

### Before (current `cache-integration` branch)

```
KAITO Helm chart
  └── Tachyon subchart (deploys cache servers, CSI, CRDs)

Workspace Controller
  └── pkg/cache/tachyon/provider.go
        ├── IsAvailable(): lists Cache CRs
        ├── PodMutations(ModelWeights):
        │     ├── Init container (copies libStorageIntercept.so from image)
        │     ├── EmptyDir volume for shared lib
        │     ├── LD_PRELOAD env var
        │     └── SI_* env vars (blob endpoint, storage path, discovery)
        └── PodMutations(KVCache):
              └── VLLM_KV_TRANSFER_CONFIG env var
```

### After (this migration)

```
Tachyon installed by user (prerequisite):
  ├── CSI DaemonSet (stages ALL libs to /opt/tachyon/lib/ on each node)
  ├── Mutating webhook (injects hostPath + env vars into labeled pods)
  └── Cache CR (provides discovery endpoint)

Workspace Controller
  └── pkg/cache/tachyon/provider.go
        ├── IsAvailable(): checks Cache CRD exists
        ├── PodMutations(ModelWeights):
        │     └── Label: tachyon.azure.com/inject: "true"
        │         (webhook handles all library/env injection)
        └── PodMutations(KVCache):
              ├── Label: tachyon.azure.com/inject: "true"
              └── VLLM_KV_TRANSFER_CONFIG env var
```

### What the CSI webhook injects (triggered by label OR PVC)

When a pod has label `tachyon.azure.com/inject: "true"` OR a Tachyon PVC:

| Injected | Purpose |
|---|---|
| `/opt/tachyon/lib/*.so` (hostPath mount, read-only) | Native libraries (libStorageIntercept, libKVClientSharedLib, libCacheClientSharedLib, libibverbs, etc.) |
| `/opt/tachyon/lib/python/` | Python packages (`py_tachyon_client` including `TachyonKVConnector`) |
| `LD_LIBRARY_PATH=/opt/tachyon/lib` | Native library discovery |
| `LD_PRELOAD=/opt/tachyon/lib/libstorageintercept.so` | Filesystem interception (only if PVC present) |
| `TACHYON_LIB_PATH=/opt/tachyon/lib` | Explicit native discovery |
| `PYTHONPATH=/opt/tachyon/lib/python` | Python package discovery |
| `STORAGE_INTERCEPT_CONFIG_FILEPATH=/etc/tachyon/storageIntercept.config` | StorageIntercept configuration |
| `/etc/tachyon/storageIntercept.config` (projected ConfigMap) | Cache server discovery, blob config, interception paths |

**Key insight**: Standard vLLM/inference images work unchanged. The webhook provides everything.

---

## Implementation Tasks

### Phase 1: Remove Deployment Responsibility

#### 1.1 Remove Helm subchart dependency
- Remove Tachyon from `charts/kaito/workspace/Chart.yaml` dependencies (if added)
- Remove Tachyon-specific templates from `charts/kaito/workspace/templates/`
- Keep `cache.providers.tachyon` section in `values.yaml` but simplify:

```yaml
# Before
cache:
  providers:
    tachyon:
      enabled: false
      discoveryEndpoint: "http://cacheserver-discovery.tachyon-cache-system.svc.cluster.local:9065"
      modelWeightsEnabled: true
      kvCacheEnabled: true
      kvConnectorProtocol: "tcp"
      blobEndpoint: ""
      blobContainer: "kaito-models"
      blobPrefix: "kaito-models"
      storageInterceptImage: "tachyontestacr.azurecr.io/cache-client-base:latest"
      storageInterceptLibPath: "/lib/libStorageIntercept.so"
      prewarmImage: ""

# After
cache:
  providers:
    tachyon:
      enabled: false
      discoveryEndpoint: ""  # Auto-discovered from Cache CR if empty
      kvCacheEnabled: true
      kvConnectorProtocol: "tcp"
      prewarmImage: ""
```

Fields removed: `modelWeightsEnabled`, `blobEndpoint`, `blobContainer`, `blobPrefix`,
`storageInterceptImage`, `storageInterceptLibPath` — all handled by CSI driver/webhook.

#### 1.2 Update documentation
- Update `docs/proposals/20260518-distributed-cache-integration.md` Installation Model section
- Update `website/docs/distributed-cache.md` prerequisites to state Tachyon is user-installed
- Add prerequisites section: "Install Tachyon CSI driver and cache servers before enabling cache"

### Phase 2: Simplify Model Weight Mutations

#### 2.1 Remove init container injection
- In `pkg/cache/tachyon/provider.go`, `PodMutations(CacheConcernModelWeights)`:
  - **Remove**: init container (`tachyon-lib-loader`)
  - **Remove**: EmptyDir volume (`tachyon-lib`)
  - **Remove**: VolumeMount for library
  - **Remove**: `SI_*` env vars (storageIntercept config)
  - **Remove**: `LD_PRELOAD` env var
  - **Add**: Pod label `tachyon.azure.com/inject: "true"` to mutations
  - **Keep**: `KAITO_MODEL_PATH` env var (vLLM `--model` path)

The label triggers the CSI webhook which handles all library injection, LD_PRELOAD,
and storageIntercept configuration.

#### 2.2 Update PodMutations struct
- Add a `Labels map[string]string` field to `PodMutations`:

```go
type PodMutations struct {
    // Labels to add to the pod template metadata.
    Labels map[string]string
    // EnvVars to inject into model containers.
    EnvVars []corev1.EnvVar
    // Volumes to add to the pod spec.
    Volumes []corev1.Volume
    // VolumeMounts to add to model containers.
    VolumeMounts []corev1.VolumeMount
    // InitContainers to prepend to the pod.
    InitContainers []corev1.Container
}
```

- Update `pkg/cache/mutations.go` (`SetCacheMutations`) to apply labels to pod template metadata.

#### 2.3 Remove Config fields
- In `pkg/cache/tachyon/provider.go`, remove from `Config` struct:
  - `ModelWeightsEnabled` (CSI handles model weight caching transparently via PVC)
  - `BlobEndpoint`
  - `BlobStorageAccountName`
  - `BlobContainer`
  - `BlobPrefix`
  - `StorageInterceptImage`
  - `StorageInterceptLibPath`
- Remove `storageInterceptEnvVars()` function
- Remove related `ConfigFromEnv()` entries
- Remove `DefaultStorageInterceptImage`, `DefaultStorageInterceptLibPath` constants

#### 2.4 Update paths.go
- `ModelLocalPath()` and related functions may still be needed if KAITO sets `--model` to a local
  path. However, with CSI driver, the path is the PVC mount path, not a constructed blob path.
- Simplify or remove `paths.go` — the model path is now just the PVC mountPath configured by the user.

#### 2.5 Update tests
- Update `pkg/cache/tachyon/provider_test.go`:
  - Remove init container assertions
  - Remove SI env var assertions
  - Add label assertion
- Update `pkg/cache/mutations_test.go` for label application
- Update `pkg/cache/integration_test.go`

### Phase 3: Simplify KV Cache Mutations

#### 3.1 Add label injection for KV cache
- In `PodMutations(CacheConcernKVCache)`:
  - **Keep**: `VLLM_KV_TRANSFER_CONFIG` env var
  - **Add**: Pod label `tachyon.azure.com/inject: "true"` (ensures native KV libs are available)

The label ensures the webhook injects `libKVClientSharedLib.so` and `py_tachyon_client`
even when there's no Tachyon PVC (KV-cache-only scenario).

#### 3.2 Auto-discover discovery endpoint
- Currently `discoveryEndpoint` comes from Helm values
- Add fallback: if empty, read from the Cache CR's status:
  ```go
  // In IsReady() or a new method:
  endpoint, _ := unstructured.NestedString(cacheObj.Object, "status", "discoveryEndpoint")
  ```
- This allows zero-config: if Tachyon is installed, KAITO finds it automatically

#### 3.3 Update kvTransferConfig
- Current format:
  ```go
  type kvTransferConfig struct {
      KVConnector  string `json:"kv_connector"`
      LocatorNodes string `json:"locator_nodes"`
      Protocol     string `json:"protocol"`
  }
  ```
- Update to match vLLM v1 KVConnector config format:
  ```go
  type kvTransferConfig struct {
      KVConnector      string                 `json:"kv_connector"`
      KVConnectorExtra map[string]interface{} `json:"kv_connector_extra_config"`
  }
  ```
  Where `kv_connector_extra_config` contains:
  ```json
  {
    "locator_nodes": "<discovery-endpoint>",
    "protocol": "rdma",
    "initial_ttl_ms": 300000,
    "producer_ttl_ms": 1800000,
    "max_ttl_ms": 86400000
  }
  ```
- The `kv_connector` value should be the full Python path:
  `py_tachyon_client.connectors.vllm_connector.TachyonKVConnector`
  (importable from PYTHONPATH injected by webhook)

### Phase 4: Update Prewarm

#### 4.1 Keep Prewarm Jobs (still valuable)
- Prewarm downloads model weights from HuggingFace and reads them through the cache (warming it)
- With CSI driver: the prewarm Job should use a Tachyon PVC for its model path
  (this triggers the CSI webhook, which intercepts reads → warms the cache)
- The prewarm Job image still needs HuggingFace download capability

#### 4.2 Simplify prewarm Job spec
- Remove `storageInterceptImage` init container from prewarm Job
- The prewarm Job should:
  1. Have a Tachyon PVC mounted (CSI webhook auto-injects LD_PRELOAD + config)
  2. Download model from HuggingFace to the PVC mount path
  3. StorageIntercept transparently warms the cache as files are written

### Phase 5: Update Proposal Document

#### 5.1 Update Installation Model section
- Replace "Helm subchart" with "user-installed prerequisite"
- Add: "Tachyon can be installed standalone or via the KAITO Helm chart as an optional subchart.
  KAITO detects it at runtime regardless of how it was installed."
- Clarify: AI Runway also detects Tachyon the same way and passes config through to KAITO.

#### 5.2 Update Architecture diagram
- Remove "Cache Backend Operator managed by KAITO"
- Show Tachyon as external prerequisite
- Show CSI webhook as the injection mechanism

#### 5.3 Add AI Runway interoperability section
- Explain: AI Runway maps `modeldeployment.spec.cache.kvCache` → `workspace.cache.kvCache`
- AI Runway's core controller detects Tachyon and writes resolved config to `status.cache.connectorConfig`
- For KAITO provider: AI Runway just passes through config — KAITO handles detection + injection natively
- For other providers (llm-d, Dynamo, KubeRay): AI Runway reads `status.cache.connectorConfig` and injects
- No coupling between KAITO and AI Runway for cache to work

---

## Dependency Graph

```
Phase 1 (Remove deployment)
  1.1 Remove Helm subchart
  1.2 Update docs
        │
        ▼
Phase 2 (Model weight simplification)
  2.1 Remove init container ──┐
  2.2 Add Labels to PodMutations ──┤
  2.3 Remove Config fields ────┤──► 2.5 Update tests
  2.4 Simplify paths.go ───────┘
        │
        ▼
Phase 3 (KV cache simplification)
  3.1 Add label injection
  3.2 Auto-discover endpoint
  3.3 Update kvTransferConfig format
        │
        ▼
Phase 4 (Prewarm update)
  4.1 Keep prewarm Jobs
  4.2 Simplify Job spec (use PVC instead of init container)
        │
        ▼
Phase 5 (Documentation)
  5.1 Update proposal
  5.2 Update architecture
  5.3 Add AI Runway section
```

---

## Files to Modify

| File | Change |
|---|---|
| `charts/kaito/workspace/values.yaml` | Remove blob/SI config, simplify tachyon section |
| `charts/kaito/workspace/templates/deployment.yaml` | Remove SI-related env var injection |
| `pkg/cache/provider.go` | Add `Labels` field to `PodMutations` |
| `pkg/cache/mutations.go` | Apply labels to pod template metadata |
| `pkg/cache/mutations_test.go` | Update assertions |
| `pkg/cache/tachyon/provider.go` | Major: remove init container, SI env, add label injection |
| `pkg/cache/tachyon/provider_test.go` | Update assertions |
| `pkg/cache/tachyon/paths.go` | Simplify or remove (PVC mount path replaces constructed path) |
| `pkg/cache/tachyon/paths_test.go` | Update or remove |
| `pkg/cache/tachyon/prewarm.go` | Update Job spec to use PVC instead of init container |
| `pkg/cache/integration_test.go` | Update for label-based assertions |
| `test/e2e/cache_integration_test.go` | Update for new behavior |
| `docs/proposals/20260518-distributed-cache-integration.md` | Update installation model, architecture |
| `website/docs/distributed-cache.md` | Update prerequisites, injection mechanism |

---

## Key Design Decisions

1. **Tachyon is a prerequisite, not a subchart** — follows the Gateway API pattern from AI Runway.
   Users install Tachyon independently. KAITO discovers it via the Cache CRD.

2. **Label-triggered injection** — `tachyon.azure.com/inject: "true"` on the pod template
   triggers the CSI webhook. This works for both model weights (PVC already triggers it) and
   KV-cache-only (no PVC, label triggers it).

3. **No custom images needed** — the webhook injects ALL libraries (native .so + Python packages)
   via hostPath. Standard vLLM images work unchanged.

4. **KAITO works standalone** — users configure `workspace.cache.kvCache` directly. No AI Runway
   dependency. AI Runway just automates this configuration for its users.

5. **Model weight caching is transparent** — handled entirely by CSI driver + webhook via PVC.
   KAITO's only responsibility is ensuring the injection label is present (for KV-cache-only cases)
   and setting `--model` to the PVC mount path.

6. **KV cache format matches vLLM v1 API** — `kv_connector` + `kv_connector_extra_config` with
   full Python module path for the connector class.

---

## Testing Strategy

- **Unit tests**: Mock Cache CR discovery, verify label injection and env var generation
- **Integration tests**: With fake Cache CR, verify PodMutations output (labels, env vars, no init containers)
- **E2E tests**: With Tachyon installed in test cluster, verify:
  - Pod gets webhook injection (hostPath mount, env vars)
  - vLLM starts with TachyonKVConnector
  - Graceful fallback when Tachyon not installed

---

## Migration Notes for Existing Users

Users on the current `cache-integration` branch who have deployed with the init-container approach:

1. Install Tachyon CSI driver + webhook separately
2. Remove `storageInterceptImage` and `blobEndpoint` from Helm values
3. Update `values.yaml` to simplified format (see Phase 1.1)
4. Pods will be recreated with label-based injection on next reconcile
