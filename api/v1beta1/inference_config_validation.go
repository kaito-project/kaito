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

package v1beta1

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// InferenceConfig represents the structure of the inference configuration
type InferenceConfig struct {
	VLLM map[string]string `yaml:"vllm"`
	// Other fields can be added as needed
}

func (w *Workspace) validateInferenceConfig(ctx context.Context) (errs *apis.FieldError) {
	// currently, this check only applies to vllm runtime
	runtime := GetWorkspaceRuntimeName(w)
	if runtime != model.RuntimeNameVLLM {
		return nil
	}

	var (
		cmName = w.Inference.Config
		cmNS   = w.Namespace
		err    error
	)
	if cmName == "" {
		klog.Infof("Inference config not specified. Using default: %q", DefaultInferenceConfigTemplate)
		cmName = DefaultInferenceConfigTemplate
		cmNS, err = utils.GetReleaseNamespace()
		if err != nil {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("Failed to determine release namespace: %v", err), "namespace"))
			return errs
		}
	}

	// Check if the ConfigMap exists
	var cm corev1.ConfigMap
	if k8sclient.Client == nil {
		errs = errs.Also(apis.ErrGeneric("Failed to obtain client from context.Context"))
		return errs
	}
	err = k8sclient.Client.Get(ctx, client.ObjectKey{Name: cmName, Namespace: cmNS}, &cm)
	if err != nil {
		if errors.IsNotFound(err) {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("ConfigMap '%s' specified in 'config' not found in namespace '%s'", cmName, cmNS), "config"))
		} else {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("Failed to get ConfigMap '%s' in namespace '%s': %v", cmName, cmNS, err), "config"))
		}
		return errs
	}

	// Check if inference_config.yaml exists
	inferenceConfigYAML, ok := cm.Data["inference_config.yaml"]
	if !ok {
		return apis.ErrMissingField("inference_config.yaml in ConfigMap")
	}

	// Check if inference_config.yaml is valid YAML
	var inferenceConfig InferenceConfig
	if err := yaml.Unmarshal([]byte(inferenceConfigYAML), &inferenceConfig); err != nil {
		return apis.ErrGeneric(fmt.Sprintf("Failed to parse inference_config.yaml: %v", err), "inference_config.yaml")
	}

	// Get SKU handler to check GPU configuration

	// Double-check that we're using vLLM runtime for the following validations
	if GetWorkspaceRuntimeName(w) == model.RuntimeNameVLLM {
		// If max-model-len is specified, validate it does not exceed the model's theoretical maximum (ModelTokenLimit)
		if rawMaxModelLen, exists := inferenceConfig.VLLM["max-model-len"]; exists && strings.TrimSpace(rawMaxModelLen) != "" {
			if w.Inference != nil && w.Inference.Preset != nil {
				presetName := strings.ToLower(string(w.Inference.Preset.Name))
				if plugin.IsValidPreset(presetName) {
					modelPreset, err := models.GetModelByName(ctx, presetName, w.Inference.Preset.PresetOptions.ModelAccessSecret, w.Namespace, k8sclient.Client)
					if err != nil {
						return apis.ErrInvalidValue(fmt.Sprintf("failed to get model preset: %v", err), "preset")
					}
					params := modelPreset.GetInferenceParameters()
					if params != nil && params.ModelTokenLimit > 0 { // Only validate when we have a positive limit
						val, err := strconv.Atoi(strings.TrimSpace(rawMaxModelLen))
						if err != nil {
							return apis.ErrInvalidValue(fmt.Sprintf("max-model-len must be an integer: %v", err), "max-model-len")
						}
						if val > params.ModelTokenLimit {
							return apis.ErrInvalidValue(
								fmt.Sprintf("max-model-len %d exceeds model's maximum supported context window %d (ModelTokenLimit)", val, params.ModelTokenLimit),
								"max-model-len",
							)
						}
					}
				}
			}
		}
	}

	return errs
}
