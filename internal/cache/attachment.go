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

package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const (
	// ConfigAnnotation names the same-namespace ConfigMap containing cache attachment intent.
	ConfigAnnotation = "kaito.sh/cache-config"
	// ConfigKey is the ConfigMap data key containing the attachment YAML document.
	ConfigKey = "cache.yaml"
)

// Attachment contains the cache concerns requested by one workload.
type Attachment struct {
	ModelWeights *ConcernConfig `yaml:"modelWeights,omitempty"`
	KVCache      *ConcernConfig `yaml:"kvCache,omitempty"`
}

// ConcernConfig selects a provider and supplies workload-scoped attachment parameters.
type ConcernConfig struct {
	Provider   ProviderName      `yaml:"provider"`
	Mode       Mode              `yaml:"mode,omitempty"`
	Parameters map[string]string `yaml:"parameters,omitempty"`
}

// HasAttachment reports whether the Workspace references a cache attachment ConfigMap.
func HasAttachment(workspace *kaitov1beta1.Workspace) bool {
	return attachmentConfigMapName(workspace) != ""
}

// ResolveAttachment loads and strictly decodes a Workspace's same-namespace attachment ConfigMap.
func ResolveAttachment(ctx context.Context, kubeClient client.Client, workspace *kaitov1beta1.Workspace) (*Attachment, error) {
	name := attachmentConfigMapName(workspace)
	if name == "" {
		return nil, nil
	}
	if kubeClient == nil {
		return nil, fmt.Errorf("resolving cache attachment %q: Kubernetes client is unavailable", name)
	}

	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: workspace.Namespace, Name: name}
	if err := kubeClient.Get(ctx, key, configMap); err != nil {
		return nil, fmt.Errorf("getting cache attachment ConfigMap %s: %w", key, err)
	}
	raw, ok := configMap.Data[ConfigKey]
	if !ok {
		return nil, fmt.Errorf("cache attachment ConfigMap %s does not contain data[%q]", key, ConfigKey)
	}

	attachment, err := decodeAttachment([]byte(raw))
	if err != nil {
		return attachment, fmt.Errorf("decoding cache attachment ConfigMap %s: %w", key, err)
	}
	return attachment, nil
}

// ResolveEffectiveAttachment applies attachment mode semantics to resolution failures.
// Invalid Opportunistic or Disabled content falls back without cache mutations;
// Required or indeterminate intent returns the resolution error and blocks callers.
func ResolveEffectiveAttachment(ctx context.Context, kubeClient client.Client, workspace *kaitov1beta1.Workspace) (*Attachment, error) {
	attachment, err := ResolveAttachment(ctx, kubeClient, workspace)
	if err == nil {
		return attachment, nil
	}
	if attachmentAllowsFallback(attachment) {
		klog.ErrorS(err, "Invalid opportunistic cache attachment; continuing without cache",
			"workspace", klog.KObj(workspace))
		return nil, nil
	}
	return attachment, err
}

func attachmentConfigMapName(workspace *kaitov1beta1.Workspace) string {
	if workspace == nil {
		return ""
	}
	return strings.TrimSpace(workspace.GetAnnotations()[ConfigAnnotation])
}

func decodeAttachment(data []byte) (*Attachment, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&document); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return nil, err
	}

	attachment := &Attachment{}
	if err := yaml.UnmarshalStrict(data, attachment); err != nil {
		return attachment, err
	}

	if attachment.ModelWeights == nil && attachment.KVCache == nil {
		return attachment, fmt.Errorf("at least one of modelWeights or kvCache must be configured")
	}
	if err := validateConcern("modelWeights", attachment.ModelWeights); err != nil {
		return attachment, err
	}
	if err := validateConcern("kvCache", attachment.KVCache); err != nil {
		return attachment, err
	}
	return attachment, nil
}

func attachmentAllowsFallback(attachment *Attachment) bool {
	if attachment == nil {
		return false
	}
	seen := false
	for _, concern := range []*ConcernConfig{attachment.ModelWeights, attachment.KVCache} {
		if concern == nil {
			continue
		}
		seen = true
		switch concern.Mode {
		case "", ModeOpportunistic, ModeDisabled:
		default:
			return false
		}
	}
	return seen
}

func validateConcern(name string, concern *ConcernConfig) error {
	if concern == nil {
		return nil
	}
	if concern.Provider == "" {
		return fmt.Errorf("%s.provider is required", name)
	}
	if concern.Mode == "" {
		concern.Mode = ModeOpportunistic
	}
	switch concern.Mode {
	case ModeRequired, ModeOpportunistic, ModeDisabled:
	default:
		return fmt.Errorf("%s.mode %q is invalid", name, concern.Mode)
	}
	return nil
}

func validateParameters(provider Provider, concern CacheConcern, parameters map[string]string) error {
	validator, ok := provider.(ParameterValidator)
	if !ok {
		if len(parameters) != 0 {
			return fmt.Errorf("provider %q does not accept attachment parameters", provider.Name())
		}
		return nil
	}
	if err := validator.ValidateParameters(concern, parameters); err != nil {
		return fmt.Errorf("provider %q parameters: %w", provider.Name(), err)
	}
	return nil
}
