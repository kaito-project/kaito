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

// Package tachyon implements the cache.Provider interface for the Tachyon
// distributed NVMe cache service. It manages Cache CRs in the tachyon-cache-system
// namespace and configures inference pods for cache access via webhook-based injection.
//
// The Tachyon CSI driver + mutating webhook handles all library staging and pod injection.
// KAITO's role is to: add the injection label, set KAITO_MODEL_PATH, and configure
// the vLLM KV transfer config.
package tachyon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

const (
	ProviderName = "tachyon"

	// CacheNamespace is the namespace where Tachyon Cache CRs are managed.
	CacheNamespace = "tachyon-cache-system"

	// Discovery endpoint for Tachyon cache servers.
	DefaultDiscoveryEndpoint = "http://cacheserver-discovery.tachyon-cache-system.svc.cluster.local:9065"
	DefaultDiscoveryPort     = 9065

	// InjectLabelKey is the pod label that triggers the Tachyon mutating webhook
	// to inject cache libraries, LD_PRELOAD, and StorageIntercept config.
	InjectLabelKey = "tachyon.azure.com/inject"
	// InjectLabelValue is the value that enables injection.
	InjectLabelValue = "true"
)

var cacheGVR = schema.GroupVersionResource{
	Group:    "storage.azure.com",
	Version:  "v1",
	Resource: "caches",
}

// Config holds Tachyon-specific configuration, typically sourced from Helm values.
type Config struct {
	// DiscoveryEndpoint overrides the default cache server discovery endpoint.
	DiscoveryEndpoint string

	// KVCacheEnabled controls whether KV caching is supported.
	KVCacheEnabled bool

	// KVConnectorProtocol is the transport protocol for KV cache (e.g., "rdma", "tcp").
	KVConnectorProtocol string

	// BlobEndpoint is the Azure Blob Storage endpoint (used by prewarm Jobs).
	BlobEndpoint string

	// BlobContainer is the blob container used for model weight storage (used by prewarm Jobs).
	BlobContainer string

	// BlobPrefix is the path prefix within the container (defaults to "kaito-models").
	BlobPrefix string

	// PrewarmImage is the container image used for prewarm Jobs.
	PrewarmImage string
}

// DefaultConfig returns sensible defaults for Tachyon integration.
func DefaultConfig() Config {
	return Config{
		DiscoveryEndpoint:   DefaultDiscoveryEndpoint,
		KVCacheEnabled:      true,
		KVConnectorProtocol: "tcp",
		BlobContainer:       "kaito-models",
		BlobPrefix:          DefaultBlobPrefix,
	}
}

// Provider implements cache.Provider for Tachyon.
type Provider struct {
	client dynamic.Interface
	config Config
}

var _ cache.Provider = (*Provider)(nil)

// New creates a Tachyon cache provider with the given dynamic client and config.
func New(client dynamic.Interface, cfg Config) *Provider {
	return &Provider{
		client: client,
		config: cfg,
	}
}

func (p *Provider) Name() string { return ProviderName }

// IsAvailable checks if the Tachyon Cache CRD is installed in the cluster.
func (p *Provider) IsAvailable(ctx context.Context) (bool, error) {
	// Attempt to list caches; a NotFound error on the resource type means CRD is missing.
	_, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		// Could be RBAC or connectivity — report as unavailable with error detail.
		return false, fmt.Errorf("checking Tachyon CRD availability: %w", err)
	}
	return true, nil
}

// IsReady checks if a Cache CR exists and has Ready=True in its status conditions.
func (p *Provider) IsReady(ctx context.Context) (bool, string, error) {
	caches, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, "", fmt.Errorf("listing Tachyon caches: %w", err)
	}
	if len(caches.Items) == 0 {
		return false, "no Tachyon Cache CR found", nil
	}

	// Check the first cache for Ready condition.
	cacheObj := caches.Items[0]
	ready, reason := checkCacheReady(&cacheObj)
	return ready, reason, nil
}

// PodMutations returns the pod-level changes needed for the requested cache concern.
// For ModelWeights: adds the Tachyon injection label and sets KAITO_MODEL_PATH.
// The Tachyon mutating webhook handles all library injection when it sees the label.
// For KVCache: adds the injection label and the vLLM KV transfer config env var.
func (p *Provider) PodMutations(ctx context.Context, concern cache.CacheConcern, workspace *kaitov1beta1.Workspace, modelName, modelRevision string) (*cache.PodMutations, error) {
	mutations := &cache.PodMutations{}

	switch concern {
	case cache.CacheConcernModelWeights:
		// Add label to trigger Tachyon webhook injection.
		mutations.Labels = map[string]string{
			InjectLabelKey: InjectLabelValue,
		}

		// Set the model path that vLLM will use as --model.
		// StorageIntercept (injected by webhook) intercepts reads under this path.
		if modelName != "" {
			localPath := ModelLocalPath(DefaultStoragePath, p.config.BlobPrefix, modelName, modelRevision)
			mutations.EnvVars = append(mutations.EnvVars, corev1.EnvVar{
				Name:  "KAITO_MODEL_PATH",
				Value: localPath,
			})
		}

	case cache.CacheConcernKVCache:
		if !p.config.KVCacheEnabled {
			return mutations, nil
		}

		// Add label to trigger Tachyon webhook injection (needed for KV connector libs).
		mutations.Labels = map[string]string{
			InjectLabelKey: InjectLabelValue,
		}

		// Resolve discovery endpoint (from config or auto-discovered from Cache CR).
		endpoint := p.resolveDiscoveryEndpoint(ctx)
		kvEnvVars, err := kvCacheEnvVars(endpoint, p.config.KVConnectorProtocol)
		if err != nil {
			return nil, fmt.Errorf("building KV cache env vars: %w", err)
		}
		mutations.EnvVars = append(mutations.EnvVars, kvEnvVars...)
	}

	return mutations, nil
}

// resolveDiscoveryEndpoint returns the discovery endpoint, auto-discovering from
// the Cache CR status if not explicitly configured.
func (p *Provider) resolveDiscoveryEndpoint(ctx context.Context) string {
	if p.config.DiscoveryEndpoint != "" {
		return p.config.DiscoveryEndpoint
	}

	// Attempt to read from Cache CR status.
	caches, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil || len(caches.Items) == 0 {
		klog.V(4).InfoS("Could not auto-discover endpoint, using default", "error", err)
		return DefaultDiscoveryEndpoint
	}

	endpoint, found, err := unstructured.NestedString(caches.Items[0].Object, "status", "discoveryEndpoint")
	if err != nil || !found || endpoint == "" {
		klog.V(4).InfoS("Cache CR has no discoveryEndpoint in status, using default")
		return DefaultDiscoveryEndpoint
	}

	klog.V(3).InfoS("Auto-discovered cache endpoint from CR status", "endpoint", endpoint)
	return endpoint
}

// Prewarm creates a prewarm Job for the specified model if one doesn't already exist.
// The Job downloads model weights from HuggingFace and uploads to the Tachyon cache.
func (p *Provider) Prewarm(ctx context.Context, req cache.PrewarmRequest) error {
	if p.config.PrewarmImage == "" {
		return fmt.Errorf("prewarm image not configured for tachyon provider")
	}
	if p.config.BlobEndpoint == "" {
		return fmt.Errorf("blob endpoint not configured for tachyon provider")
	}

	klog.V(2).InfoS("Prewarm triggered", "model", req.ModelName, "revision", req.ModelRevision)
	// The Job is created by the workspace controller using BuildPrewarmJob().
	// This method validates that the config is sufficient to build a prewarm Job.
	return nil
}

// Cleanup is a placeholder for cache invalidation logic.
func (p *Provider) Cleanup(ctx context.Context, req cache.PrewarmRequest) error {
	klog.V(4).InfoS("Cleanup requested (not yet implemented)", "model", req.ModelName, "source", req.ModelSource)
	return nil
}

// kvTransferConfig is the JSON structure expected by vLLM v1's --kv-transfer-config flag.
type kvTransferConfig struct {
	KVConnector           string                 `json:"kv_connector"`
	KVConnectorExtraConfig map[string]interface{} `json:"kv_connector_extra_config"`
}

// kvCacheEnvVars returns the env var for vLLM KV transfer config (v1 format).
func kvCacheEnvVars(discoveryEndpoint, protocol string) ([]corev1.EnvVar, error) {
	cfg := kvTransferConfig{
		KVConnector: "py_tachyon_client.connectors.vllm_connector.TachyonKVConnector",
		KVConnectorExtraConfig: map[string]interface{}{
			"locator_nodes":   discoveryEndpoint,
			"protocol":        protocol,
			"initial_ttl_ms":  300000,
			"producer_ttl_ms": 1800000,
			"max_ttl_ms":      86400000,
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []corev1.EnvVar{
		{Name: "VLLM_KV_TRANSFER_CONFIG", Value: string(data)},
	}, nil
}

// checkCacheReady inspects an unstructured Cache CR for the Ready condition.
func checkCacheReady(obj *unstructured.Unstructured) (bool, string) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, "no status conditions found"
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		if condType == "Ready" {
			status, _ := cond["status"].(string)
			reason, _ := cond["reason"].(string)
			if status == string(metav1.ConditionTrue) {
				return true, "cache is ready"
			}
			return false, fmt.Sprintf("cache not ready: %s", reason)
		}
	}
	return false, "Ready condition not found"
}

// ConfigFromEnv builds a Config from environment variables (set via Helm chart).
// Falls back to DefaultConfig() for any unset values.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("TACHYON_DISCOVERY_ENDPOINT"); v != "" {
		cfg.DiscoveryEndpoint = v
	}
	if v := os.Getenv("TACHYON_KV_CACHE_ENABLED"); v != "" {
		cfg.KVCacheEnabled = v == "true"
	}
	if v := os.Getenv("TACHYON_KV_CONNECTOR_PROTOCOL"); v != "" {
		cfg.KVConnectorProtocol = v
	}
	if v := os.Getenv("TACHYON_BLOB_ENDPOINT"); v != "" {
		cfg.BlobEndpoint = v
	}
	if v := os.Getenv("TACHYON_BLOB_CONTAINER"); v != "" {
		cfg.BlobContainer = v
	}
	if v := os.Getenv("TACHYON_BLOB_PREFIX"); v != "" {
		cfg.BlobPrefix = v
	}
	if v := os.Getenv("TACHYON_PREWARM_IMAGE"); v != "" {
		cfg.PrewarmImage = v
	}

	return cfg
}
