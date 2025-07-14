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

package v1alpha1

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
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

	// Check if required fields are present
	modelLenRequired := false

	// Get SKU handler to check GPU configuration
	skuHandler, err := utils.GetSKUHandler()
	if err != nil {
		return apis.ErrGeneric(fmt.Sprintf("Failed to get SKU handler: %v", err), "instanceType")
	}
	if skuConfig := skuHandler.GetGPUConfigBySKU(w.Resource.InstanceType); skuConfig != nil {
		// Check if this is a multi-GPU instance with less than 20GB per GPU
		gpuMemPerGPU := skuConfig.GPUMemGB / skuConfig.GPUCount
		// For multi-GPU instances with less than 20GB per GPU, max-model-len is required
		if skuConfig.GPUCount > 1 && gpuMemPerGPU < 20 {
			modelLenRequired = true
		}
	}
	if w.Resource.Count != nil && *w.Resource.Count > 1 {
		modelLenRequired = true
	}

	if modelLenRequired {
		maxModelLen, exists := inferenceConfig.VLLM["max-model-len"]
		if !exists || maxModelLen == "" {
			return apis.ErrMissingField("max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU or distributed inference")
		}
	}

	return errs
}
