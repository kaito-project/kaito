// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package v1alpha1

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils"
)

// InferenceConfig represents the structure of the inference configuration
type InferenceConfig struct {
	VLLM map[string]string `yaml:"vllm"`
	// Other fields can be added as needed
}

func (i *InferenceSpec) validateConfigMap(ctx context.Context, namespace, cmName, instanceType string) (errs *apis.FieldError) {
	var cm corev1.ConfigMap
	if k8sclient.Client == nil {
		errs = errs.Also(apis.ErrGeneric("Failed to obtain client from context.Context"))
		return errs
	}
	err := k8sclient.Client.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, &cm)
	if err != nil {
		if errors.IsNotFound(err) {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("ConfigMap '%s' specified in 'config' not found in namespace '%s'", cmName, namespace), "config"))
		} else {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("Failed to get ConfigMap '%s' in namespace '%s': %v", cmName, namespace, err), "config"))
		}
		return errs
	}

	// Check if inference_config.yaml exists
	inferenceConfigYAML, ok := cm.Data["inference_config.yaml"]
	if !ok {
		return apis.ErrMissingField("inference_config.yaml in ConfigMap")
	}

	// Parse the inference config
	var inferenceConfig InferenceConfig
	if err := yaml.Unmarshal([]byte(inferenceConfigYAML), &inferenceConfig); err != nil {
		return apis.ErrGeneric(fmt.Sprintf("Failed to parse inference_config.yaml: %v", err), "inference_config.yaml")
	}

	// Get SKU handler to check GPU configuration
	skuHandler, err := utils.GetSKUHandler()
	if err != nil {
		return apis.ErrGeneric(fmt.Sprintf("Failed to get SKU handler: %v", err), "instanceType")
	}

	if skuConfig := skuHandler.GetGPUConfigBySKU(instanceType); skuConfig != nil {
		// Check if this is a multi-GPU instance with less than 20GB per GPU
		gpuMemPerGPU := skuConfig.GPUMemGB / skuConfig.GPUCount
		if skuConfig.GPUCount > 1 && gpuMemPerGPU < 20 {
			// For multi-GPU instances with less than 20GB per GPU, max-model-len is required
			maxModelLen, exists := inferenceConfig.VLLM["max-model-len"]
			if !exists || maxModelLen == "" {
				return apis.ErrMissingField("max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU")
			}
		}
	}

	return errs
}
