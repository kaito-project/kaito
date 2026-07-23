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
// The DACS mutating webhook handles library injection when it sees the inject label.
// KAITO's role is to: add the injection label, mount the client library image volume,
// set RunAI streamer env vars for cache integration, and configure vLLM KV transfer.
package dacs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
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

	// Fallback defaults used when no DiscoveryEndpoint is configured.
	defaultCacheNamespace = "dacs-cache-system"
	defaultCacheName      = "cache-sample"
	defaultDiscoveryPort  = 9065

	ClientVolumeName = "cache-client"
	ClientMountPath  = "/opt/cache-client"
	ClientLibPath    = ClientMountPath + "/usr/local/lib/python3.10/dist-packages/dacs_client/libStorageDirect.so"

	// runaiStreamerMarker identifies a workload that loads model weights through
	// the run:ai model streamer (e.g. --load-format=runai_streamer). DACS's
	// model-weights cache hooks that streamer's read path, so it can only engage
	// when the streamer is in use.
	runaiStreamerMarker = "runai_streamer"

	// InjectLabelKey is the pod label that triggers the DACS mutating webhook
	// to inject cache libraries and configuration.
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
	//
	// glibc compatibility: the DACS client (libStorageDirect.so) is built and
	// packaged against glibc 2.35 (manylinux_2_35). The inference/base image
	// consuming this cache MUST therefore be glibc >= 2.35 (Ubuntu 22.04+),
	// otherwise the runai streamer will fail to dlopen the library at runtime.
	ClientImage string

	// KVCacheEnabled controls whether KV caching is supported.
	KVCacheEnabled bool

	// KVConnectorProtocol is the transport protocol for KV cache (e.g., "rdma", "tcp").
	KVConnectorProtocol string
}

// DefaultConfig returns sensible defaults for DACS integration.
// DiscoveryEndpoint is left empty to enable auto-discovery from the Cache CR.
func DefaultConfig() Config {
	return Config{
		ClientImage:         os.Getenv("DACS_CLIENT_IMAGE"),
		KVCacheEnabled:      true,
		KVConnectorProtocol: "tcp",
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

// cacheNamespace returns the namespace for Cache CRs.
// If DiscoveryEndpoint is set (e.g. "cache-sample-discovery.my-ns.svc.cluster.local"),
// the namespace is extracted from the second DNS label. Otherwise falls back to the default.
func (p *Provider) cacheNamespace() string {
	if ep := p.config.DiscoveryEndpoint; ep != "" {
		if _, ns, ok := parseDiscoveryEndpoint(ep); ok {
			return ns
		}
	}
	return defaultCacheNamespace
}

// cacheName returns the default Cache CR name.
// If DiscoveryEndpoint is set, the name is extracted by stripping the "-discovery"
// suffix from the first DNS label. Otherwise falls back to the default.
func (p *Provider) cacheName() string {
	if ep := p.config.DiscoveryEndpoint; ep != "" {
		if name, _, ok := parseDiscoveryEndpoint(ep); ok {
			return name
		}
	}
	return defaultCacheName
}

// parseDiscoveryEndpoint extracts the cache name and namespace from a discovery
// endpoint of the form "<name>-discovery.<namespace>.svc.cluster.local[:port]".
// Returns (cacheName, namespace, true) on success.
func parseDiscoveryEndpoint(endpoint string) (string, string, bool) {
	// Strip port if present.
	host := endpoint
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	parts := strings.SplitN(host, ".", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	svcName := parts[0]
	ns := parts[1]
	name := strings.TrimSuffix(svcName, "-discovery")
	if name == svcName {
		// No "-discovery" suffix found; can't extract cache name.
		return "", "", false
	}
	return name, ns, true
}

// IsAvailable checks if the DACS Cache CRD is installed in the cluster.
// If cacheName is provided, checks that specific CR exists; otherwise tries
// the default cache name, then falls back to single-CR auto-detection.
func (p *Provider) IsAvailable(ctx context.Context, cacheName string) (bool, error) {
	ns := p.cacheNamespace()
	if cacheName != "" {
		_, err := p.client.Resource(cacheGVR).Namespace(ns).Get(ctx, cacheName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("checking DACS cache %q: %w", cacheName, err)
		}
		return true, nil
	}

	// Try default cache name first.
	defName := p.cacheName()
	_, err := p.client.Resource(cacheGVR).Namespace(ns).Get(ctx, defName, metav1.GetOptions{})
	if err == nil {
		return true, nil
	}
	if !errors.IsNotFound(err) {
		return false, fmt.Errorf("checking DACS cache %q: %w", defName, err)
	}

	// Default not found — list to see what exists.
	caches, err := p.client.Resource(cacheGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
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
			len(caches.Items), defName)
	}
}

// IsReady checks cache readiness. If cacheName is provided, checks that specific
// CR. Otherwise tries the default cache name; if absent, auto-detects a single CR.
// Returns error if multiple CRs exist without a default or explicit cacheName.
func (p *Provider) IsReady(ctx context.Context, cacheName string) (bool, string, error) {
	ns := p.cacheNamespace()
	if cacheName != "" {
		cacheObj, err := p.client.Resource(cacheGVR).Namespace(ns).Get(ctx, cacheName, metav1.GetOptions{})
		if err != nil {
			return false, "", fmt.Errorf("getting DACS cache %q: %w", cacheName, err)
		}
		ready, reason := checkCacheReady(cacheObj)
		return ready, reason, nil
	}

	// Try default cache name first.
	defName := p.cacheName()
	cacheObj, err := p.client.Resource(cacheGVR).Namespace(ns).Get(ctx, defName, metav1.GetOptions{})
	if err == nil {
		p.cacheObj = cacheObj
		ready, reason := checkCacheReady(cacheObj)
		return ready, reason, nil
	}
	if !errors.IsNotFound(err) {
		return false, "", fmt.Errorf("getting DACS cache %q: %w", defName, err)
	}

	// Default not found — list to auto-detect.
	caches, err := p.client.Resource(cacheGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
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
			len(caches.Items), defName)
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
		// No model-specific defaults needed (model path is user-specified via az:// in workspace YAML).
	case "kv":
		defaults["kvConnectorProtocol"] = p.config.KVConnectorProtocol
	}
	return defaults
}

// PodMutations returns the pod-level changes needed for the requested cache concern.
// cacheName identifies the target cache CR; if empty, uses the default (p.cacheObj).
// For ModelWeights: adds the DACS injection label, mounts the client library,
// and sets RunAI streamer env vars to enable cache-backed model loading.
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
			return nil, fmt.Errorf("DACS client image not configured: set cache.providers.dacs.clientImage in Helm values or DACS_CLIENT_IMAGE env var")
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

		// Tell run:ai model streamer to load the cache library from the ImageVolume.
		discoveryEndpoint := p.resolveDiscoveryEndpoint(cacheName)
		mutations.EnvVars = append(mutations.EnvVars,
			corev1.EnvVar{Name: "RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB", Value: ClientLibPath},
			corev1.EnvVar{Name: "RUNAI_STREAMER_CACHE_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "CACHE_DISCOVERY_URL", Value: discoveryEndpoint},
			corev1.EnvVar{Name: "CACHE_SERVER_PORT", Value: fmt.Sprintf("%d", defaultDiscoveryPort)},
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

// AppliesTo implements cache.PodApplicabilityChecker. DACS's model-weights cache
// only engages when the inference container loads the model through the run:ai
// model streamer (--load-format=runai_streamer); the injected client library and
// RUNAI_STREAMER_* env vars are a silent no-op on any other load path. It scans
// the rendered workload's container command/args for the streamer marker and
// declines otherwise, so — even in Required mode — KAITO does not block on a
// cache that could never take effect.
//
// The framework consults AppliesTo for the model-weights concern only; KV cache
// is presence-based (an in-process vLLM connector with no model-load-path
// dependency), so this governs model-weights applicability.
func (p *Provider) AppliesTo(_ cache.CacheConcern, _ *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) bool {
	if ss == nil {
		return false
	}
	for _, c := range ss.Spec.Template.Spec.Containers {
		for _, arg := range append(append([]string{}, c.Command...), c.Args...) {
			if strings.Contains(arg, runaiStreamerMarker) {
				return true
			}
		}
	}
	return false
}

// Priority: explicit config > provided cacheName > default cacheObj name > derived default.
func (p *Provider) resolveDiscoveryEndpoint(cacheName string) string {
	if p.config.DiscoveryEndpoint != "" {
		return p.config.DiscoveryEndpoint
	}

	ns := p.cacheNamespace()

	// Use provided cacheName (from user ConfigMap).
	if cacheName != "" {
		return fmt.Sprintf("%s-discovery.%s.svc.cluster.local", cacheName, ns)
	}

	// Fall back to the default cacheObj discovered by IsReady.
	if p.cacheObj != nil {
		return fmt.Sprintf("%s-discovery.%s.svc.cluster.local", p.cacheObj.GetName(), ns)
	}

	defName := p.cacheName()
	return fmt.Sprintf("%s-discovery.%s.svc.cluster.local", defName, ns)
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
	if v := os.Getenv("DACS_CLIENT_IMAGE"); v != "" {
		cfg.ClientImage = v
	}

	return cfg
}
