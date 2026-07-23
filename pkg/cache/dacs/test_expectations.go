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
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

// init registers the DACS provider's conformance expectations so the
// provider-agnostic conformance suite automatically discovers and verifies it
// whenever this package is imported. All DACS-specific assertions (env var names,
// injection label, ImageVolume, discovery URL and KV transfer config shape) live
// here, keeping provider specifics out of the shared conformance suite.
func init() {
	cache.RegisterExpectations(cache.Expectations{
		Provider: kaitov1beta1.CacheProvider(ProviderName),
		// PodMutations does not touch the API server, so a nil client is safe for
		// offline mutation conformance.
		NewForConformance: func() cache.Provider {
			cfg := DefaultConfig()
			cfg.ClientImage = "test.azurecr.io/dacs-client:latest"
			return New(nil, cfg)
		},
		// E2EExempt defaults to false, so dacs is exercised by the online e2e suite.
		ModelWeights: cache.MutationExpectation{
			Supported: true,
			RequiredLabels: map[string]string{
				InjectLabelKey: InjectLabelValue,
			},
			RequiredEnvVars: []string{
				"RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED",
				"RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB",
				"RUNAI_STREAMER_CACHE_ENABLED",
				"CACHE_DISCOVERY_URL",
				"CACHE_SERVER_PORT",
			},
			RequiredVolumes:      []string{ClientVolumeName},
			RequiredVolumeMounts: []string{ClientVolumeName},
			Validate:             validateModelWeightsMutations,
		},
		KVCache: cache.MutationExpectation{
			Supported: true,
			RequiredLabels: map[string]string{
				InjectLabelKey: InjectLabelValue,
			},
			RequiredEnvVars: []string{
				"VLLM_KV_TRANSFER_CONFIG",
			},
			Validate: validateKVCacheMutations,
		},
		E2EScenarios: dacsE2EScenarios(),
		CacheWarm:    dacsCacheWarm,
		E2EWorkspace: dacsE2EWorkspace,
	})
}

// validateModelWeightsMutations performs DACS-specific deep validation of the
// model weights mutations (library path, discovery URL, server port).
func validateModelWeightsMutations(m *cache.PodMutations) []error {
	var errs []error

	if lib, ok := m.EnvValue("RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB"); !ok || lib != ClientLibPath {
		errs = append(errs, fmt.Errorf("RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB = %q, want %q", lib, ClientLibPath))
	}

	if url, ok := m.EnvValue("CACHE_DISCOVERY_URL"); !ok || url == "" {
		errs = append(errs, fmt.Errorf("CACHE_DISCOVERY_URL must be a non-empty discovery endpoint"))
	}

	wantPort := fmt.Sprintf("%d", defaultDiscoveryPort)
	if port, ok := m.EnvValue("CACHE_SERVER_PORT"); !ok || port != wantPort {
		errs = append(errs, fmt.Errorf("CACHE_SERVER_PORT = %q, want %q", port, wantPort))
	}

	return errs
}

// validateKVCacheMutations performs DACS-specific deep validation of the KV cache
// mutations (the VLLM_KV_TRANSFER_CONFIG must be valid JSON naming the DACS connector).
func validateKVCacheMutations(m *cache.PodMutations) []error {
	var errs []error

	raw, ok := m.EnvValue("VLLM_KV_TRANSFER_CONFIG")
	if !ok || raw == "" {
		return []error{fmt.Errorf("VLLM_KV_TRANSFER_CONFIG must be a non-empty JSON value")}
	}

	var cfg struct {
		KVConnector            string                 `json:"kv_connector"`
		KVConnectorExtraConfig map[string]interface{} `json:"kv_connector_extra_config"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return []error{fmt.Errorf("VLLM_KV_TRANSFER_CONFIG is not valid JSON: %w", err)}
	}
	if cfg.KVConnector == "" {
		errs = append(errs, fmt.Errorf("VLLM_KV_TRANSFER_CONFIG missing kv_connector"))
	}
	if _, ok := cfg.KVConnectorExtraConfig["locator_nodes"]; !ok {
		errs = append(errs, fmt.Errorf("VLLM_KV_TRANSFER_CONFIG missing kv_connector_extra_config.locator_nodes"))
	}

	return errs
}

// dacsE2EWorkspace supplies the DACS-specific serving pod template the shared e2e
// conformance specs apply on top of their neutral workspace skeleton. All DACS
// serving specifics (RunAI streamer image, load format, model URL, Azure storage
// account) live here, so the shared e2e suite carries no DACS details. When any
// required env var is unset the provider's workspace-building specs are skipped.
func dacsE2EWorkspace() *cache.E2EWorkspaceConfig {
	image := os.Getenv("DACS_CACHE_VLLM_IMAGE")
	if image == "" {
		return &cache.E2EWorkspaceConfig{SkipReason: "DACS_CACHE_VLLM_IMAGE not set"}
	}
	modelURL := os.Getenv("DACS_CACHE_MODEL_URL")
	if modelURL == "" {
		return &cache.E2EWorkspaceConfig{SkipReason: "DACS_CACHE_MODEL_URL not set"}
	}
	storageAccount := os.Getenv("DACS_CACHE_STORAGE_ACCOUNT")
	if storageAccount == "" {
		return &cache.E2EWorkspaceConfig{SkipReason: "DACS_CACHE_STORAGE_ACCOUNT not set"}
	}
	return &cache.E2EWorkspaceConfig{
		Customizer: dacsWorkspaceCustomizer(image, modelURL, storageAccount),
	}
}

// dacsWorkspaceCustomizer returns the function that stamps the DACS serving
// specifics (image, RunAI streamer load path, Azure storage account) onto the
// first container of a workspace. It is shared by the workspace-building e2e specs
// and the cache-warm data-plane scenario so both stay in sync. An empty image
// leaves the skeleton image untouched.
func dacsWorkspaceCustomizer(image, modelURL, storageAccount string) func(ws *kaitov1beta1.Workspace) {
	return func(ws *kaitov1beta1.Workspace) {
		if ws.Inference == nil || ws.Inference.Template == nil ||
			len(ws.Inference.Template.Spec.Containers) == 0 {
			return
		}
		c := &ws.Inference.Template.Spec.Containers[0]
		if image != "" {
			c.Image = image
		}
		c.Args = []string{
			"--model=" + modelURL,
			"--dtype=float32",
			"--max-model-len=512",
			"--load-format=runai_streamer",
			"--model-loader-extra-config={\"concurrency\":8}",
		}
		found := false
		for i := range c.Env {
			if c.Env[i].Name == "AZURE_STORAGE_ACCOUNT_NAME" {
				c.Env[i].Value = storageAccount
				found = true
				break
			}
		}
		if !found {
			c.Env = append(c.Env, corev1.EnvVar{
				Name:  "AZURE_STORAGE_ACCOUNT_NAME",
				Value: storageAccount,
			})
		}
	}
}

// dacsCacheWarm checks DACS-specific prerequisites and returns the cache warm
// test configuration. All validation (env vars, log patterns) lives here so the
// generic e2e runner has no DACS-specific logic.
func dacsCacheWarm() *cache.CacheWarmConfig {
	modelURL := os.Getenv("DACS_CACHE_MODEL_URL")
	if modelURL == "" {
		return &cache.CacheWarmConfig{SkipReason: "DACS_CACHE_MODEL_URL not set"}
	}
	storageAccount := os.Getenv("DACS_CACHE_STORAGE_ACCOUNT")
	if storageAccount == "" {
		return &cache.CacheWarmConfig{SkipReason: "DACS_CACHE_STORAGE_ACCOUNT not set"}
	}
	// Namespace where workload identity federation is already configured for
	// the vllm-sa ServiceAccount. Falls back to "default" so that test runs
	// don't depend on random e2e namespace names.
	cacheNS := os.Getenv("DACS_CACHE_NAMESPACE")
	if cacheNS == "" {
		cacheNS = "default"
	}
	return &cache.CacheWarmConfig{
		Namespace:           cacheNS,
		WorkspaceCustomizer: dacsWorkspaceCustomizer(os.Getenv("DACS_CACHE_VLLM_IMAGE"), modelURL, storageAccount),
		ValidatePreWarm: func(h cache.E2EHarness, ws *kaitov1beta1.Workspace) error {
			logs, err := h.PodLogs(ws.Name + "-0")
			if err != nil {
				return fmt.Errorf("reading pre-warm pod logs: %w", err)
			}
			n, found := parseCacheStatValue(logs, "RemoteClient")
			if !found {
				return fmt.Errorf("pre-warm pod logs do not contain %q stat; cache may not be warming", "RemoteClient=")
			}
			if n == 0 {
				n, found = parseCacheStatValue(logs, "RemoteCache")
				if found && n == 0 {
					return fmt.Errorf("pre-warm pod logs show RemoteCache=0; expected >0 blob reads during warm-up")
				}
			}
			return nil
		},
		ValidatePostWarm: func(h cache.E2EHarness, ws *kaitov1beta1.Workspace) error {
			logs, err := h.PodLogs(ws.Name + "-0")
			if err != nil {
				return fmt.Errorf("reading post-warm pod logs: %w", err)
			}
			n, found := parseCacheStatValue(logs, "RemoteCache")
			if !found {
				return fmt.Errorf("post-warm pod logs do not contain %q stat; cache may not be hitting", "RemoteCache=")
			}
			if n == 0 {
				return fmt.Errorf("post-warm pod logs show RemoteCache=0; expected >0 cache hits on warm reload")
			}
			return nil
		},
	}
}

// parseCacheStatValue extracts the integer value for a key like "RemoteCache=807"
// from DACS StreamingClient log lines. Returns (value, true) on success.
var cacheStatRe = regexp.MustCompile(`(\w+)=(\d+)`)

func parseCacheStatValue(logs, key string) (int, bool) {
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, key+"=") {
			continue
		}
		for _, match := range cacheStatRe.FindAllStringSubmatch(line, -1) {
			if match[1] == key {
				n, err := strconv.Atoi(match[2])
				if err == nil {
					return n, true
				}
			}
		}
	}
	return 0, false
}
