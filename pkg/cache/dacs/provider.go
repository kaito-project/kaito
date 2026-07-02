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

// Package dacs implements the cache.Provider interface for the DACS
// distributed NVMe cache service. It manages Cache CRs in the dacs-cache-system
// namespace and configures inference pods for cache access via webhook-based injection.
//
// The DACS CSI driver + mutating webhook handles all library staging and pod injection.
// KAITO's role is to: add the injection label, set KAITO_MODEL_PATH, and configure
// the vLLM KV transfer config.
package dacs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

const (
	ProviderName = "dacs"

	// CacheNamespace is the namespace where DACS Cache CRs are managed.
	CacheNamespace = "dacs-cache-system"

	// DefaultCacheName is the default name for the DACS Cache CR.
	DefaultCacheName = "cache-sample"

	// Discovery endpoint for DACS cache servers (hostname only, no scheme or port).
	DefaultDiscoveryEndpoint = DefaultCacheName + "-discovery." + CacheNamespace + ".svc.cluster.local"
	DefaultDiscoveryPort     = 9065

	// ImageVolume configuration for the cache client sidecar libraries.
	DefaultClientImage = "hariazstortest.azurecr.io/dacs-client:20260701.7"
	ClientVolumeName   = "cache-client"
	ClientMountPath    = "/opt/cache-client"
	ClientLibPath      = ClientMountPath + "/usr/local/lib/python3.10/dist-packages/dacs_client/libStorageDirect.so"

	// InjectLabelKey is the pod label that triggers the DACS mutating webhook
	// to inject cache libraries, LD_PRELOAD, and StorageIntercept config.
	InjectLabelKey = "dacs.azure.com/inject"
	// InjectLabelValue is the value that enables injection.
	InjectLabelValue = "true"
)

var cacheGVR = schema.GroupVersionResource{
	Group:    "storage.azure.com",
	Version:  "v1",
	Resource: "caches",
}

// Config holds DACS-specific configuration, typically sourced from Helm values.
type Config struct {
	// DiscoveryEndpoint overrides the default cache server discovery endpoint.
	DiscoveryEndpoint string

	// ClientImage is the OCI image containing the DACS client libraries,
	// mounted as an ImageVolume into inference pods.
	ClientImage string

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

// DefaultConfig returns sensible defaults for DACS integration.
func DefaultConfig() Config {
	return Config{
		DiscoveryEndpoint:   DefaultDiscoveryEndpoint,
		ClientImage:         DefaultClientImage,
		KVCacheEnabled:      true,
		KVConnectorProtocol: "tcp",
		BlobContainer:       "kaito-models",
		BlobPrefix:          DefaultBlobPrefix,
	}
}

// Provider implements cache.Provider for DACS.
type Provider struct {
	client   dynamic.Interface
	config   Config
	cacheObj *unstructured.Unstructured // last discovered Cache CR (for event emission and endpoint resolution)
}

var _ cache.Provider = (*Provider)(nil)
var _ cache.EventTarget = (*Provider)(nil)
var _ cache.DefaultConfigProvider = (*Provider)(nil)

// New creates a DACS cache provider with the given dynamic client and config.
func New(client dynamic.Interface, cfg Config) *Provider {
	return &Provider{
		client: client,
		config: cfg,
	}
}

func (p *Provider) Name() string { return ProviderName }

// IsAvailable checks if the DACS Cache CRD is installed in the cluster.
// If cacheName is provided, checks that specific CR exists; otherwise tries
// DefaultCacheName, then falls back to single-CR auto-detection.
func (p *Provider) IsAvailable(ctx context.Context, cacheName string) (bool, error) {
	if cacheName != "" {
		_, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).Get(ctx, cacheName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("checking DACS cache %q: %w", cacheName, err)
		}
		return true, nil
	}

	// Try default cache name first.
	_, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).Get(ctx, DefaultCacheName, metav1.GetOptions{})
	if err == nil {
		return true, nil
	}
	if !errors.IsNotFound(err) {
		return false, fmt.Errorf("checking DACS cache %q: %w", DefaultCacheName, err)
	}

	// Default not found — list to see what exists.
	caches, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking DACS CRD availability: %w", err)
	}

	switch len(caches.Items) {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("multiple DACS cache clusters found (%d) but no default %q; "+
			"specify cacheName in the Config ConfigMap via InferenceSet or Workspace CR",
			len(caches.Items), DefaultCacheName)
	}
}

// IsReady checks cache readiness. If cacheName is provided, checks that specific
// CR. Otherwise tries DefaultCacheName; if absent, auto-detects a single CR.
// Returns error if multiple CRs exist without a default or explicit cacheName.
func (p *Provider) IsReady(ctx context.Context, cacheName string) (bool, string, error) {
	if cacheName != "" {
		cacheObj, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).Get(ctx, cacheName, metav1.GetOptions{})
		if err != nil {
			return false, "", fmt.Errorf("getting DACS cache %q: %w", cacheName, err)
		}
		ready, reason := checkCacheReady(cacheObj)
		return ready, reason, nil
	}

	// Try default cache name first.
	cacheObj, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).Get(ctx, DefaultCacheName, metav1.GetOptions{})
	if err == nil {
		p.cacheObj = cacheObj
		ready, reason := checkCacheReady(cacheObj)
		return ready, reason, nil
	}
	if !errors.IsNotFound(err) {
		return false, "", fmt.Errorf("getting DACS cache %q: %w", DefaultCacheName, err)
	}

	// Default not found — list to auto-detect.
	caches, err := p.client.Resource(cacheGVR).Namespace(CacheNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, "", fmt.Errorf("listing DACS caches: %w", err)
	}

	switch len(caches.Items) {
	case 0:
		return false, "no DACS Cache CR found", nil
	case 1:
		p.cacheObj = &caches.Items[0]
		ready, reason := checkCacheReady(p.cacheObj)
		return ready, reason, nil
	default:
		return false, "", fmt.Errorf("multiple DACS cache clusters found (%d) but no default %q; "+
			"specify cacheName in the Config ConfigMap via InferenceSet or Workspace CR",
			len(caches.Items), DefaultCacheName)
	}
}

// EventObject returns the Cache CR for Kubernetes event emission.
func (p *Provider) EventObject() runtime.Object {
	if p.cacheObj == nil {
		return nil
	}
	return p.cacheObj
}

// DefaultConfig returns Helm-value defaults for the given concern, used
// as the base layer when merging with user-provided Config ConfigMaps.
func (p *Provider) DefaultConfig(concern string) map[string]string {
	defaults := map[string]string{
		"discoveryEndpoint": p.config.DiscoveryEndpoint,
		"provider":          ProviderName,
	}
	switch concern {
	case "model":
		defaults["blobEndpoint"] = p.config.BlobEndpoint
		defaults["blobContainer"] = p.config.BlobContainer
		defaults["blobPrefix"] = p.config.BlobPrefix
	case "kv":
		defaults["kvConnectorProtocol"] = p.config.KVConnectorProtocol
	}
	return defaults
}

// PodMutations returns the pod-level changes needed for the requested cache concern.
// cacheName identifies the target cache CR; if empty, uses the default (p.cacheObj).
// For ModelWeights: adds the DACS injection label and sets KAITO_MODEL_PATH.
// The DACS mutating webhook handles all library injection when it sees the label.
// For KVCache: adds the injection label and the vLLM KV transfer config env var.
func (p *Provider) PodMutations(ctx context.Context, concern cache.CacheConcern, workspace *kaitov1beta1.Workspace, modelName, modelRevision, cacheName string) (*cache.PodMutations, error) {
	mutations := &cache.PodMutations{}

	switch concern {
	case cache.CacheConcernModelWeights:
		// Add label to trigger DACS webhook injection.
		mutations.Labels = map[string]string{
			InjectLabelKey: InjectLabelValue,
		}

		// Mount the DACS client image as an ImageVolume.
		clientImage := p.config.ClientImage
		if clientImage == "" {
			clientImage = DefaultClientImage
		}
		mutations.Volumes = append(mutations.Volumes, corev1.Volume{
			Name: ClientVolumeName,
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{
					Reference:  clientImage,
					PullPolicy: corev1.PullIfNotPresent,
				},
			},
		})
		mutations.VolumeMounts = append(mutations.VolumeMounts, corev1.VolumeMount{
			Name:      ClientVolumeName,
			MountPath: ClientMountPath,
			ReadOnly:  true,
		})

		// Set the model path that vLLM will use as --model.
		// StorageIntercept (injected by webhook) intercepts reads under this path.
		if modelName != "" {
			localPath := ModelLocalPath(DefaultStoragePath, p.config.BlobPrefix, modelName, modelRevision)
			mutations.EnvVars = append(mutations.EnvVars, corev1.EnvVar{
				Name:  "KAITO_MODEL_PATH",
				Value: localPath,
			})
		}

		// Tell run:ai model streamer to load the cache library from the ImageVolume.
		discoveryEndpoint := p.resolveDiscoveryEndpoint(cacheName)
		mutations.EnvVars = append(mutations.EnvVars,
			corev1.EnvVar{Name: "RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB", Value: ClientLibPath},
			corev1.EnvVar{Name: "LD_LIBRARY_PATH", Value: ClientMountPath + "/usr/lib/x86_64-linux-gnu:" + ClientMountPath + "/usr/local/lib/python3.10/dist-packages/dacs_client"},
			corev1.EnvVar{Name: "RUNAI_STREAMER_CACHE_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "CACHE_DISCOVERY_URL", Value: discoveryEndpoint},
			corev1.EnvVar{Name: "CACHE_SERVER_PORT", Value: fmt.Sprintf("%d", DefaultDiscoveryPort)},
		)

	case cache.CacheConcernKVCache:
		if !p.config.KVCacheEnabled {
			return mutations, nil
		}

		// Add label to trigger DACS webhook injection (needed for KV connector libs).
		mutations.Labels = map[string]string{
			InjectLabelKey: InjectLabelValue,
		}

		// Resolve discovery endpoint from provided cacheName or default.
		endpoint := p.resolveDiscoveryEndpoint(cacheName)
		kvEnvVars, err := kvCacheEnvVars(endpoint, p.config.KVConnectorProtocol)
		if err != nil {
			return nil, fmt.Errorf("building KV cache env vars: %w", err)
		}
		mutations.EnvVars = append(mutations.EnvVars, kvEnvVars...)
	}

	return mutations, nil
}

// resolveDiscoveryEndpoint returns the discovery endpoint for a given cache.
// Priority: explicit config > provided cacheName > default cacheObj name > DefaultDiscoveryEndpoint.
func (p *Provider) resolveDiscoveryEndpoint(cacheName string) string {
	if p.config.DiscoveryEndpoint != "" {
		return p.config.DiscoveryEndpoint
	}

	// Use provided cacheName (from user ConfigMap).
	if cacheName != "" {
		return fmt.Sprintf("%s-discovery.%s.svc.cluster.local", cacheName, CacheNamespace)
	}

	// Fall back to the default cacheObj discovered by IsReady.
	if p.cacheObj != nil {
		return fmt.Sprintf("%s-discovery.%s.svc.cluster.local", p.cacheObj.GetName(), CacheNamespace)
	}

	return DefaultDiscoveryEndpoint
}

// Cleanup is a placeholder for cache invalidation logic.
// TODO(distributed-cache): Implement cache invalidation — evict cached model chunks
// from DACS cache servers when workspace is deleted with cleanupOnDelete: true.
func (p *Provider) Cleanup(ctx context.Context, workspace *kaitov1beta1.Workspace, modelName string) error {
	klog.V(4).InfoS("Cleanup requested (not yet implemented)", "workspace", workspace.Name, "model", modelName)
	return nil
}

// kvTransferConfig is the JSON structure expected by vLLM v1's --kv-transfer-config flag.
type kvTransferConfig struct {
	KVConnector            string                 `json:"kv_connector"`
	KVConnectorExtraConfig map[string]interface{} `json:"kv_connector_extra_config"`
}

// kvCacheEnvVars returns the env var for vLLM KV transfer config (v1 format).
func kvCacheEnvVars(discoveryEndpoint, protocol string) ([]corev1.EnvVar, error) {
	cfg := kvTransferConfig{
		KVConnector: "dacs_client.connectors.vllm_connector.DacsKVConnector",
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

	if v := os.Getenv("DACS_DISCOVERY_ENDPOINT"); v != "" {
		cfg.DiscoveryEndpoint = v
	}
	if v := os.Getenv("DACS_KV_CACHE_ENABLED"); v != "" {
		cfg.KVCacheEnabled = v == "true"
	}
	if v := os.Getenv("DACS_KV_CONNECTOR_PROTOCOL"); v != "" {
		cfg.KVConnectorProtocol = v
	}
	if v := os.Getenv("DACS_BLOB_ENDPOINT"); v != "" {
		cfg.BlobEndpoint = v
	}
	if v := os.Getenv("DACS_BLOB_CONTAINER"); v != "" {
		cfg.BlobContainer = v
	}
	if v := os.Getenv("DACS_BLOB_PREFIX"); v != "" {
		cfg.BlobPrefix = v
	}
	if v := os.Getenv("DACS_PREWARM_IMAGE"); v != "" {
		cfg.PrewarmImage = v
	}

	return cfg
}
