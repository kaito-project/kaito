// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tachyon

import (
"context"
"encoding/json"
"testing"

metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
"k8s.io/apimachinery/pkg/runtime"
"k8s.io/apimachinery/pkg/runtime/schema"
dynamicfake "k8s.io/client-go/dynamic/fake"

kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
"github.com/kaito-project/kaito/pkg/cache"
)

func newFakeProvider(objects ...runtime.Object) *Provider {
scheme := runtime.NewScheme()
client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
map[schema.GroupVersionResource]string{
cacheGVR: "CacheList",
}, objects...)
return New(client, DefaultConfig())
}

func newReadyCache() *unstructured.Unstructured {
return &unstructured.Unstructured{
Object: map[string]interface{}{
"apiVersion": "storage.azure.com/v1",
"kind":       "Cache",
"metadata": map[string]interface{}{
"name":      "test-cache",
"namespace": CacheNamespace,
},
"status": map[string]interface{}{
"conditions": []interface{}{
map[string]interface{}{
"type":   "Ready",
"status": "True",
"reason": "CacheReady",
},
},
},
},
}
}

func TestProviderName(t *testing.T) {
p := newFakeProvider()
if p.Name() != ProviderName {
t.Errorf("expected %q, got %q", ProviderName, p.Name())
}
}

func TestPodMutations_ModelWeights(t *testing.T) {
p := newFakeProvider()
ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeOpportunistic,
},
},
}

mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

// Should have the injection label.
if mutations.Labels == nil {
t.Fatal("expected labels, got nil")
}
if mutations.Labels[InjectLabelKey] != InjectLabelValue {
t.Errorf("label %s: got %q, want %q", InjectLabelKey, mutations.Labels[InjectLabelKey], InjectLabelValue)
}

// Should have KAITO_MODEL_PATH env var only.
if len(mutations.EnvVars) != 1 {
t.Fatalf("expected 1 env var (KAITO_MODEL_PATH), got %d: %v", len(mutations.EnvVars), mutations.EnvVars)
}
if mutations.EnvVars[0].Name != "KAITO_MODEL_PATH" {
t.Errorf("env var name: got %q, want %q", mutations.EnvVars[0].Name, "KAITO_MODEL_PATH")
}
expectedPath := "/mnt/models/kaito-models/microsoft/phi-4/main"
if mutations.EnvVars[0].Value != expectedPath {
t.Errorf("KAITO_MODEL_PATH: got %q, want %q", mutations.EnvVars[0].Value, expectedPath)
}

// Should NOT have init containers, volumes, or volume mounts (webhook handles these).
if len(mutations.InitContainers) != 0 {
t.Errorf("expected 0 init containers (webhook handles injection), got %d", len(mutations.InitContainers))
}
if len(mutations.Volumes) != 0 {
t.Errorf("expected 0 volumes, got %d", len(mutations.Volumes))
}
if len(mutations.VolumeMounts) != 0 {
t.Errorf("expected 0 volume mounts, got %d", len(mutations.VolumeMounts))
}
}

func TestPodMutations_ModelWeightsCustomPrefix(t *testing.T) {
scheme := runtime.NewScheme()
client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
map[schema.GroupVersionResource]string{
cacheGVR: "CacheList",
})
cfg := DefaultConfig()
cfg.BlobPrefix = "custom-prefix"
p := New(client, cfg)

ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeOpportunistic,
},
},
}

mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "abc123")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

if len(mutations.EnvVars) != 1 {
t.Fatalf("expected 1 env var, got %d", len(mutations.EnvVars))
}
expectedPath := "/mnt/models/custom-prefix/microsoft/phi-4/abc123"
if mutations.EnvVars[0].Value != expectedPath {
t.Errorf("KAITO_MODEL_PATH: got %q, want %q", mutations.EnvVars[0].Value, expectedPath)
}
}

func TestPodMutations_ModelWeightsNoModelName(t *testing.T) {
p := newFakeProvider()
ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeOpportunistic,
},
},
}

mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "", "")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

// Label still set even without model name.
if mutations.Labels[InjectLabelKey] != InjectLabelValue {
t.Error("injection label should be set even without model name")
}

// No KAITO_MODEL_PATH when model name is empty.
if len(mutations.EnvVars) != 0 {
t.Errorf("expected 0 env vars when model name empty, got %d", len(mutations.EnvVars))
}
}

func TestPodMutations_KVCacheOnly(t *testing.T) {
p := newFakeProvider()
ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
KVCache: &kaitov1beta1.KVCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeRequired,
},
},
}

mutations, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "", "")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

// Should have injection label.
if mutations.Labels == nil || mutations.Labels[InjectLabelKey] != InjectLabelValue {
t.Error("expected injection label for KV cache concern")
}

// Should have VLLM_KV_TRANSFER_CONFIG env var.
if len(mutations.EnvVars) != 1 {
t.Fatalf("expected 1 env var for KV cache, got %d", len(mutations.EnvVars))
}
if mutations.EnvVars[0].Name != "VLLM_KV_TRANSFER_CONFIG" {
t.Errorf("expected VLLM_KV_TRANSFER_CONFIG, got %s", mutations.EnvVars[0].Name)
}

// Verify vLLM v1 format with kv_connector_extra_config.
var cfg kvTransferConfig
if err := json.Unmarshal([]byte(mutations.EnvVars[0].Value), &cfg); err != nil {
t.Fatalf("failed to parse KV config: %v", err)
}
if cfg.KVConnector != "py_tachyon_client.connectors.vllm_connector.TachyonKVConnector" {
t.Errorf("kv_connector: got %q, want full Python path", cfg.KVConnector)
}
if cfg.KVConnectorExtraConfig == nil {
t.Fatal("expected kv_connector_extra_config, got nil")
}
if cfg.KVConnectorExtraConfig["protocol"] != "tcp" {
t.Errorf("protocol: got %v, want tcp", cfg.KVConnectorExtraConfig["protocol"])
}
if cfg.KVConnectorExtraConfig["locator_nodes"] != DefaultDiscoveryEndpoint {
t.Errorf("locator_nodes: got %v, want %s", cfg.KVConnectorExtraConfig["locator_nodes"], DefaultDiscoveryEndpoint)
}

// Should NOT have init containers.
if len(mutations.InitContainers) != 0 {
t.Errorf("expected 0 init containers, got %d", len(mutations.InitContainers))
}
}

func TestPodMutations_BothConcerns(t *testing.T) {
p := newFakeProvider()
ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeOpportunistic,
},
KVCache: &kaitov1beta1.KVCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeOpportunistic,
},
},
}

// Model weights concern: label + KAITO_MODEL_PATH only.
mwMutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main")
if err != nil {
t.Fatalf("model weights: unexpected error: %v", err)
}
if len(mwMutations.EnvVars) != 1 || mwMutations.EnvVars[0].Name != "KAITO_MODEL_PATH" {
t.Errorf("model weights should only have KAITO_MODEL_PATH, got %v", mwMutations.EnvVars)
}
for _, env := range mwMutations.EnvVars {
if env.Name == "VLLM_KV_TRANSFER_CONFIG" {
t.Error("model weights concern should not include KV config")
}
}

// KV concern: label + VLLM_KV_TRANSFER_CONFIG only.
kvMutations, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "microsoft/phi-4", "main")
if err != nil {
t.Fatalf("KV cache: unexpected error: %v", err)
}
if len(kvMutations.EnvVars) != 1 || kvMutations.EnvVars[0].Name != "VLLM_KV_TRANSFER_CONFIG" {
t.Errorf("KV cache should only have VLLM_KV_TRANSFER_CONFIG, got %v", kvMutations.EnvVars)
}
if len(kvMutations.InitContainers) != 0 {
t.Error("KV concern should not include init containers")
}
}

func TestPodMutations_KVCacheDisabled(t *testing.T) {
scheme := runtime.NewScheme()
client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
map[schema.GroupVersionResource]string{
cacheGVR: "CacheList",
})
cfg := DefaultConfig()
cfg.KVCacheEnabled = false
p := New(client, cfg)

ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
KVCache: &kaitov1beta1.KVCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeRequired,
},
},
}

kv, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "", "")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if len(kv.EnvVars) != 0 {
t.Errorf("expected 0 env vars when KV cache disabled, got %d", len(kv.EnvVars))
}
if len(kv.Labels) != 0 {
t.Errorf("expected 0 labels when KV cache disabled, got %d", len(kv.Labels))
}
}

func TestPodMutations_ConcernIsolation(t *testing.T) {
p := newFakeProvider()
ws := &kaitov1beta1.Workspace{
Cache: &kaitov1beta1.CacheSpec{
ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeRequired,
},
KVCache: &kaitov1beta1.KVCacheConfig{
Provider: "tachyon",
Mode:     kaitov1beta1.CacheModeRequired,
},
},
}

// KV concern must not return model weight env vars.
kv, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "microsoft/phi-4", "main")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if len(kv.InitContainers) != 0 {
t.Errorf("KV concern should not have init containers, got %d", len(kv.InitContainers))
}
for _, env := range kv.EnvVars {
if env.Name == "KAITO_MODEL_PATH" {
t.Errorf("KV concern should not include %s", env.Name)
}
}

// Model weights concern must not return KV env vars.
mw, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
for _, env := range mw.EnvVars {
if env.Name == "VLLM_KV_TRANSFER_CONFIG" {
t.Error("model weights concern should not include VLLM_KV_TRANSFER_CONFIG")
}
}
}

func TestCheckCacheReady(t *testing.T) {
tests := []struct {
name      string
obj       *unstructured.Unstructured
wantReady bool
wantMsg   string
}{
{
name:      "ready cache",
obj:       newReadyCache(),
wantReady: true,
wantMsg:   "cache is ready",
},
{
name: "not ready cache",
obj: &unstructured.Unstructured{
Object: map[string]interface{}{
"status": map[string]interface{}{
"conditions": []interface{}{
map[string]interface{}{
"type":   "Ready",
"status": "False",
"reason": "CacheInitializing",
},
},
},
},
},
wantReady: false,
wantMsg:   "cache not ready: CacheInitializing",
},
{
name: "no conditions",
obj: &unstructured.Unstructured{
Object: map[string]interface{}{
"status": map[string]interface{}{},
},
},
wantReady: false,
wantMsg:   "no status conditions found",
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
ready, msg := checkCacheReady(tt.obj)
if ready != tt.wantReady {
t.Errorf("ready: got %v, want %v", ready, tt.wantReady)
}
if msg != tt.wantMsg {
t.Errorf("msg: got %q, want %q", msg, tt.wantMsg)
}
})
}
}

func TestIsReady_NoCaches(t *testing.T) {
p := newFakeProvider()
ready, reason, err := p.IsReady(context.Background())
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if ready {
t.Error("expected not ready when no caches exist")
}
if reason != "no Tachyon Cache CR found" {
t.Errorf("unexpected reason: %s", reason)
}
}

func TestIsReady_WithReadyCache(t *testing.T) {
cacheObj := newReadyCache()

scheme := runtime.NewScheme()
client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
map[schema.GroupVersionResource]string{
cacheGVR: "CacheList",
}, cacheObj)
p := New(client, DefaultConfig())

ready, reason, err := p.IsReady(context.Background())
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !ready {
t.Errorf("expected ready, got not ready: %s", reason)
}
}

func TestExplicitRegistration(t *testing.T) {
p := newFakeProvider()
cache.Register(p)

got, err := cache.Get(kaitov1beta1.CacheProvider(ProviderName))
if err != nil {
t.Fatalf("tachyon provider not registered: %v", err)
}
if got.Name() != ProviderName {
t.Errorf("registered provider name: got %q, want %q", got.Name(), ProviderName)
}
}

// Suppress unused import warning.
var _ = metav1.Now
