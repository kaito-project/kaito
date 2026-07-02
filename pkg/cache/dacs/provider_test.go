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

package dacs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

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
	return newCache(DefaultCacheName)
}

func newCache(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "storage.azure.com/v1",
			"kind":       "Cache",
			"metadata": map[string]interface{}{
				"name":      name,
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

func envVarMap(envVars []corev1.EnvVar) map[string]string {
	values := make(map[string]string, len(envVars))
	for _, envVar := range envVars {
		values[envVar.Name] = envVar.Value
	}
	return values
}

func TestProviderName(t *testing.T) {
	p := newFakeProvider()
	if p.Name() != ProviderName {
		t.Errorf("expected %q, got %q", ProviderName, p.Name())
	}
}

func TestIsAvailable(t *testing.T) {
	tests := []struct {
		name      string
		objects   []runtime.Object
		setup     func(*dynamicfake.FakeDynamicClient)
		wantOK    bool
		wantErr   bool
		errSubstr string
	}{
		{
			name: "not found returns unavailable without error",
			setup: func(client *dynamicfake.FakeDynamicClient) {
				client.PrependReactor("get", "caches", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewNotFound(cacheGVR.GroupResource(), "cache-sample")
				})
				client.PrependReactor("list", "caches", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewNotFound(cacheGVR.GroupResource(), "caches")
				})
			},
			wantOK: false,
		},
		{
			name:    "single cache returns available",
			setup:   func(*dynamicfake.FakeDynamicClient) {},
			objects: []runtime.Object{newCache("my-cache")},
			wantOK:  true,
		},
		{
			name: "non notfound error is returned",
			setup: func(client *dynamicfake.FakeDynamicClient) {
				client.PrependReactor("list", "caches", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, context.DeadlineExceeded
				})
			},
			wantOK:    false,
			wantErr:   true,
			errSubstr: "checking DACS CRD availability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
				map[schema.GroupVersionResource]string{
					cacheGVR: "CacheList",
				}, tt.objects...)
			tt.setup(client)
			p := New(client, DefaultConfig())

			ok, err := p.IsAvailable(context.Background(), "")
			if ok != tt.wantOK {
				t.Fatalf("available: got %v, want %v", ok, tt.wantOK)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultConfigProvider(t *testing.T) {
	cfg := Config{
		DiscoveryEndpoint:   "discovery.example",
		KVCacheEnabled:      true,
		KVConnectorProtocol: "rdma",
		BlobEndpoint:        "https://blob.example",
		BlobContainer:       "weights",
		BlobPrefix:          "custom-prefix",
		PrewarmImage:        "prewarm:latest",
	}
	p := New(newFakeProvider().client, cfg)

	modelDefaults := p.DefaultConfig("model")
	if modelDefaults["provider"] != ProviderName {
		t.Fatalf("provider: got %q, want %q", modelDefaults["provider"], ProviderName)
	}
	if modelDefaults["discoveryEndpoint"] != cfg.DiscoveryEndpoint {
		t.Fatalf("discoveryEndpoint: got %q, want %q", modelDefaults["discoveryEndpoint"], cfg.DiscoveryEndpoint)
	}
	if modelDefaults["blobEndpoint"] != cfg.BlobEndpoint {
		t.Fatalf("blobEndpoint: got %q, want %q", modelDefaults["blobEndpoint"], cfg.BlobEndpoint)
	}
	if modelDefaults["blobContainer"] != cfg.BlobContainer {
		t.Fatalf("blobContainer: got %q, want %q", modelDefaults["blobContainer"], cfg.BlobContainer)
	}
	if modelDefaults["blobPrefix"] != cfg.BlobPrefix {
		t.Fatalf("blobPrefix: got %q, want %q", modelDefaults["blobPrefix"], cfg.BlobPrefix)
	}

	kvDefaults := p.DefaultConfig("kv")
	if kvDefaults["provider"] != ProviderName {
		t.Fatalf("provider: got %q, want %q", kvDefaults["provider"], ProviderName)
	}
	if kvDefaults["discoveryEndpoint"] != cfg.DiscoveryEndpoint {
		t.Fatalf("discoveryEndpoint: got %q, want %q", kvDefaults["discoveryEndpoint"], cfg.DiscoveryEndpoint)
	}
	if kvDefaults["kvConnectorProtocol"] != cfg.KVConnectorProtocol {
		t.Fatalf("kvConnectorProtocol: got %q, want %q", kvDefaults["kvConnectorProtocol"], cfg.KVConnectorProtocol)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("DACS_DISCOVERY_ENDPOINT", "env-discovery.example")
	t.Setenv("DACS_KV_CACHE_ENABLED", "false")
	t.Setenv("DACS_KV_CONNECTOR_PROTOCOL", "rdma")
	t.Setenv("DACS_BLOB_ENDPOINT", "https://blob.env.example")
	t.Setenv("DACS_BLOB_CONTAINER", "env-container")
	t.Setenv("DACS_BLOB_PREFIX", "env-prefix")
	t.Setenv("DACS_PREWARM_IMAGE", "env-prewarm:latest")

	cfg := ConfigFromEnv()
	if cfg.DiscoveryEndpoint != "env-discovery.example" {
		t.Fatalf("DiscoveryEndpoint: got %q", cfg.DiscoveryEndpoint)
	}
	if cfg.KVCacheEnabled {
		t.Fatal("KVCacheEnabled: got true, want false")
	}
	if cfg.KVConnectorProtocol != "rdma" {
		t.Fatalf("KVConnectorProtocol: got %q", cfg.KVConnectorProtocol)
	}
	if cfg.BlobEndpoint != "https://blob.env.example" {
		t.Fatalf("BlobEndpoint: got %q", cfg.BlobEndpoint)
	}
	if cfg.BlobContainer != "env-container" {
		t.Fatalf("BlobContainer: got %q", cfg.BlobContainer)
	}
	if cfg.BlobPrefix != "env-prefix" {
		t.Fatalf("BlobPrefix: got %q", cfg.BlobPrefix)
	}
	if cfg.PrewarmImage != "env-prewarm:latest" {
		t.Fatalf("PrewarmImage: got %q", cfg.PrewarmImage)
	}
}

func TestPodMutations_ModelWeights(t *testing.T) {
	p := newFakeProvider()
	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}

	mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main", "")
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

	// Should have KAITO_MODEL_PATH plus DACS discovery env vars.
	if len(mutations.EnvVars) != 7 {
		t.Fatalf("expected 7 env vars, got %d: %v", len(mutations.EnvVars), mutations.EnvVars)
	}
	envVars := envVarMap(mutations.EnvVars)
	expectedPath := "/mnt/models/kaito-models/microsoft/phi-4/main"
	if envVars["KAITO_MODEL_PATH"] != expectedPath {
		t.Errorf("KAITO_MODEL_PATH: got %q, want %q", envVars["KAITO_MODEL_PATH"], expectedPath)
	}
	if envVars["RUNAI_STREAMER_CACHE_ENABLED"] != "true" {
		t.Errorf("RUNAI_STREAMER_CACHE_ENABLED: got %q, want true", envVars["RUNAI_STREAMER_CACHE_ENABLED"])
	}
	if !strings.Contains(envVars["CACHE_DISCOVERY_URL"], DefaultDiscoveryEndpoint) {
		t.Errorf("CACHE_DISCOVERY_URL: got %q, want it to contain %q", envVars["CACHE_DISCOVERY_URL"], DefaultDiscoveryEndpoint)
	}
	if envVars["CACHE_SERVER_PORT"] != "9065" {
		t.Errorf("CACHE_SERVER_PORT: got %q, want 9065", envVars["CACHE_SERVER_PORT"])
	}

	// Should NOT have init containers, volumes, or volume mounts (webhook handles these).
	if len(mutations.InitContainers) != 0 {
		t.Errorf("expected 0 init containers (webhook handles injection), got %d", len(mutations.InitContainers))
	}
	if len(mutations.Volumes) != 1 {
		t.Errorf("expected 1 volume (DACS client), got %d", len(mutations.Volumes))
	}
	if len(mutations.VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount (DACS client), got %d", len(mutations.VolumeMounts))
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
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}

	mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "abc123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mutations.EnvVars) != 7 {
		t.Fatalf("expected 7 env vars, got %d", len(mutations.EnvVars))
	}
	envVars := envVarMap(mutations.EnvVars)
	expectedPath := "/mnt/models/custom-prefix/microsoft/phi-4/abc123"
	if envVars["KAITO_MODEL_PATH"] != expectedPath {
		t.Errorf("KAITO_MODEL_PATH: got %q, want %q", envVars["KAITO_MODEL_PATH"], expectedPath)
	}
}

func TestPodMutations_ModelWeightsNoModelName(t *testing.T) {
	p := newFakeProvider()
	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}

	mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Label still set even without model name.
	if mutations.Labels[InjectLabelKey] != InjectLabelValue {
		t.Error("injection label should be set even without model name")
	}

	// No KAITO_MODEL_PATH when model name is empty.
	if len(mutations.EnvVars) != 6 {
		t.Errorf("expected 6 env vars when model name empty, got %d", len(mutations.EnvVars))
	}
}

func TestPodMutations_KVCacheOnly(t *testing.T) {
	p := newFakeProvider()
	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	mutations, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "", "", "")
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
	if cfg.KVConnector != "dacs_client.connectors.vllm_connector.DacsKVConnector" {
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
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}

	// Model weights concern: label + KAITO_MODEL_PATH only.
	mwMutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main", "")
	if err != nil {
		t.Fatalf("model weights: unexpected error: %v", err)
	}
	if len(mwMutations.EnvVars) != 7 {
		t.Errorf("model weights should have KAITO_MODEL_PATH + RUNAI env vars, got %v", mwMutations.EnvVars)
	}
	for _, env := range mwMutations.EnvVars {
		if env.Name == "VLLM_KV_TRANSFER_CONFIG" {
			t.Error("model weights concern should not include KV config")
		}
	}

	// KV concern: label + VLLM_KV_TRANSFER_CONFIG only.
	kvMutations, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "microsoft/phi-4", "main", "")
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
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	kv, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "", "", "")
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
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	// KV concern must not return model weight env vars.
	kv, err := p.PodMutations(context.Background(), cache.CacheConcernKVCache, ws, "microsoft/phi-4", "main", "")
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
	mw, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, ws, "microsoft/phi-4", "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, env := range mw.EnvVars {
		if env.Name == "VLLM_KV_TRANSFER_CONFIG" {
			t.Error("model weights concern should not include VLLM_KV_TRANSFER_CONFIG")
		}
	}
}

func TestResolveDiscoveryEndpoint_FromCacheName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DiscoveryEndpoint = ""
	p := New(dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			cacheGVR: "CacheList",
		}, newCache("my-cache")), cfg)

	// IsReady() populates p.cacheObj which resolveDiscoveryEndpoint uses
	p.IsReady(context.Background(), "")

	// Empty cacheName — falls back to cacheObj name discovered by IsReady.
	endpoint := p.resolveDiscoveryEndpoint("")
	if endpoint != "my-cache-discovery.dacs-cache-system.svc.cluster.local" {
		t.Fatalf("expected auto-discovered endpoint, got %q", endpoint)
	}

	// Explicit cacheName overrides cacheObj.
	endpoint = p.resolveDiscoveryEndpoint("custom-cache")
	if endpoint != "custom-cache-discovery.dacs-cache-system.svc.cluster.local" {
		t.Fatalf("expected custom endpoint, got %q", endpoint)
	}
}

func TestResolveDiscoveryEndpoint_FallbackToDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DiscoveryEndpoint = ""
	p := New(dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			cacheGVR: "CacheList",
		}), cfg)

	endpoint := p.resolveDiscoveryEndpoint("")
	if endpoint != DefaultDiscoveryEndpoint {
		t.Fatalf("expected default endpoint, got %q", endpoint)
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
	ready, reason, err := p.IsReady(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected not ready when no caches exist")
	}
	if reason != "no DACS Cache CR found" {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestEventObject(t *testing.T) {
	p := newFakeProvider(newReadyCache())
	if got := p.EventObject(); got != nil {
		t.Fatalf("expected nil event object before IsReady, got %#v", got)
	}

	ready, reason, err := p.IsReady(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready cache, got ready=%v reason=%q", ready, reason)
	}

	obj := p.EventObject()
	if obj == nil {
		t.Fatal("expected cached event object after IsReady")
	}
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		t.Fatalf("expected *unstructured.Unstructured, got %T", obj)
	}
	if unstructuredObj.GetName() != DefaultCacheName {
		t.Fatalf("event object name: got %q, want %s", unstructuredObj.GetName(), DefaultCacheName)
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

	ready, reason, err := p.IsReady(context.Background(), "")
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
		t.Fatalf("dacs provider not registered: %v", err)
	}
	if got.Name() != ProviderName {
		t.Errorf("registered provider name: got %q, want %q", got.Name(), ProviderName)
	}
}

// Suppress unused import warning.
var _ = metav1.Now
