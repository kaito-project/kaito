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

package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/generator"
)

const (
	// CustomModelConfigKey is the ConfigMap entry holding the model configuration.
	CustomModelConfigKey = "config.json"

	// CustomModelSizeKey is the ConfigMap entry holding the on-disk size of the
	// weight bundle, in bytes.
	//
	// The size is supplied rather than derived from config.json because deriving
	// it means re-implementing every architecture's tensor layout, which fails by
	// producing a believable wrong number instead of an error. The operator
	// uploading the bundle can read the true size directly from storage.
	CustomModelSizeKey = "model_size_bytes"
)

// CustomModelName returns the registry key for a configuration digest.
func CustomModelName(digest string) string {
	return plugin.CustomModelNamePrefix + digest
}

// ConfigDigest returns the lowercase hexadecimal SHA-256 of the exact
// configuration bytes. Hashing the raw bytes rather than a reserialized form
// keeps the digest stable and independent of any JSON normalization.
func ConfigDigest(configJSON []byte) string {
	sum := sha256.Sum256(configJSON)
	return hex.EncodeToString(sum[:])
}

// ResolvedCustomModel is the outcome of resolving a bring-your-own model.
type ResolvedCustomModel struct {
	// Model is the registered, ready-to-use model instance.
	Model model.Model
	// Digest is the SHA-256 of the configuration it was derived from.
	Digest string
	// Name is the content-addressed registry key, i.e. "custom-<digest>".
	// The declared size is not part of it: correcting a size does not make this
	// a different model.
	Name string
	// SizeBytes is the declared on-disk size of the weight bundle. It is
	// reported separately from Digest because it is not covered by it: Digest
	// must stay a digest of config.json alone so that startup verification can
	// compare it against the bundle's own copy of that file.
	SizeBytes int64
}

// parseModelSize reads the declared bundle size from the ConfigMap. It is
// validated here, at the single point where the ConfigMap is read, because the
// value flows into resource.MustParse further downstream, which panics rather
// than returning an error - so a malformed entry would take down the controller
// instead of rejecting the object.
func parseModelSize(cm *corev1.ConfigMap) (int64, error) {
	raw, ok := cm.Data[CustomModelSizeKey]
	if !ok {
		return 0, fmt.Errorf("ConfigMap %q does not contain %q, which must give the on-disk size of the model bundle in bytes", cm.Name, CustomModelSizeKey)
	}

	size, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q in ConfigMap %q must be a whole number of bytes, got %q", CustomModelSizeKey, cm.Name, raw)
	}
	if err := validateModelSize(size); err != nil {
		return 0, err
	}
	return size, nil
}

// validateModelSize rejects a declared bundle size that no operator would mean.
// It is shared by the ConfigMap path and the config-only entry point so both
// reject the same unusable values before they reach the panicking downstream
// consumer.
func validateModelSize(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return fmt.Errorf("%q must be greater than zero, got %d", CustomModelSizeKey, sizeBytes)
	}
	if sizeBytes > maxCustomModelSizeBytes {
		return fmt.Errorf("%q is %d bytes, which exceeds the %d byte ceiling; this is almost certainly a unit mistake", CustomModelSizeKey, sizeBytes, int64(maxCustomModelSizeBytes))
	}
	return nil
}

// maxCustomModelSizeBytes is a sanity ceiling, not a supported-size limit. Its
// purpose is to catch a value entered in the wrong unit, which would otherwise
// be accepted and used to request an impossible amount of capacity.
const maxCustomModelSizeBytes = 1 << 50 // 1 PiB

// ResolveCustomModel resolves a bring-your-own model from the inference
// ConfigMap in the given namespace, registering it under a content-addressed
// name so that runtime parameters, CLI rendering and node estimation can look
// it up exactly as they do preset and HuggingFace models.
//
// The registry is an in-memory, process-local cache. Content addressing is what
// makes that safe: because the ConfigMap is immutable, a cache miss is repaired
// by re-deriving the identical result from the same bytes, so a restarted
// controller needs no additional durable state.
func ResolveCustomModel(ctx context.Context, kubeClient client.Client, configMapName, namespace string) (*ResolvedCustomModel, error) {
	if configMapName == "" {
		return nil, fmt.Errorf("preset %q requires 'inference.config' to reference a ConfigMap containing %s", plugin.PresetNameCustom, CustomModelConfigKey)
	}
	if kubeClient == nil {
		return nil, fmt.Errorf("no Kubernetes client available to read ConfigMap %q", configMapName)
	}

	cm := &corev1.ConfigMap{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: namespace}, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q in namespace %q: %w", configMapName, namespace, err)
	}

	return ResolveCustomModelFromConfigMap(cm)
}

// ResolveCustomModelFromConfigMap resolves a bring-your-own model from an
// already retrieved ConfigMap, layering the ConfigMap-specific read and
// immutability check on top of ResolveCustomModelFromConfig.
func ResolveCustomModelFromConfigMap(cm *corev1.ConfigMap) (*ResolvedCustomModel, error) {
	if cm.Immutable == nil || !*cm.Immutable {
		return nil, fmt.Errorf("ConfigMap %q must be immutable: a custom model's configuration and runtime settings are fixed for the lifetime of the deployment, so changing either requires a new ConfigMap", cm.Name)
	}

	raw, ok := cm.Data[CustomModelConfigKey]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %q does not contain %q, which is required by preset %q", cm.Name, CustomModelConfigKey, plugin.PresetNameCustom)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("ConfigMap %q contains an empty %q", cm.Name, CustomModelConfigKey)
	}

	sizeBytes, err := parseModelSize(cm)
	if err != nil {
		return nil, err
	}

	resolved, err := ResolveCustomModelFromConfig([]byte(raw), sizeBytes)
	if err != nil {
		return nil, fmt.Errorf("ConfigMap %q: %w", cm.Name, err)
	}
	return resolved, nil
}

// ResolveCustomModelFromConfig resolves a bring-your-own model from raw
// config.json bytes and the declared on-disk bundle size, registering it under a
// content-addressed name so that runtime parameters, CLI rendering and node
// estimation can look it up exactly as they do preset and HuggingFace models.
//
// This is the source-independent core of custom-model resolution. Systems that
// have no Kubernetes ConfigMap - for example a caller that only wants a node
// estimate for a config.json it already holds - resolve through here directly
// and pass the returned Model to the estimator; ResolveCustomModel and
// ResolveCustomModelFromConfigMap are the Kubernetes-backed wrappers.
//
// The registry is an in-memory, process-local cache. Content addressing is what
// makes that safe: the same bytes and size always re-derive the identical
// result, so a restart needs no durable state.
func ResolveCustomModelFromConfig(configJSON []byte, sizeBytes int64) (*ResolvedCustomModel, error) {
	if len(bytes.TrimSpace(configJSON)) == 0 {
		return nil, fmt.Errorf("%q is required and must not be empty", CustomModelConfigKey)
	}
	if err := validateModelSize(sizeBytes); err != nil {
		return nil, err
	}

	digest := ConfigDigest(configJSON)
	name := CustomModelName(digest)

	// The generated parameters are a function of the configuration *and* the
	// declared size, so the cache is keyed by both. The model's identity - its
	// Name, its digest, and everything the serving pod verifies against - stays
	// the configuration digest alone; only this internal registry key carries
	// the size. Keying by the digest alone would make two deployments that share
	// a config.json but declare different sizes (e.g. the same model resolved in
	// two namespaces) repeatedly evict and rebuild each other's process-global
	// entry on every reconcile. A size-qualified key gives each (config, size)
	// pair its own slot, so a corrected size is never served stale figures and
	// concurrent resolutions never thrash.
	cacheKey := customModelCacheKey(name, sizeBytes)
	if m := plugin.KaitoModelRegister.MustGet(cacheKey); m != nil {
		return &ResolvedCustomModel{Model: m, Digest: digest, Name: name, SizeBytes: sizeBytes}, nil
	}

	param, err := generator.GenerateFromConfig(name, configJSON, sizeBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", CustomModelConfigKey, err)
	}

	// The lookup above and the register below are not one atomic operation, so
	// two concurrent resolutions of the same (config, size) can both miss and
	// both register. That is harmless precisely because the key is content plus
	// size: they generate byte-identical parameters, so the second registration
	// overwrites the first with an equal value. The registry is a cache, not a
	// lock; correctness rests on the derivation being pure, not on single-flight.
	return &ResolvedCustomModel{Model: registerModel(cacheKey, param), Digest: digest, Name: name, SizeBytes: sizeBytes}, nil
}

// customModelCacheKey is the process-global registry key under which a resolved
// custom model is cached. It intentionally differs from the model's served name
// (CustomModelName, the digest alone): the generated parameters depend on the
// declared size as well as the configuration, so the cache key must too. The
// registered instance still reports Metadata.Name as the digest-only identity,
// so decoding it with model.CustomModelDigest and verifying the streamed bundle
// against it are unaffected.
func customModelCacheKey(name string, sizeBytes int64) string {
	return fmt.Sprintf("%s-%d", name, sizeBytes)
}
