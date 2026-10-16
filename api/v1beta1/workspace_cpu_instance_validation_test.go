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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestValidateCreateWithInference_AllowsCPUNAPInstanceTypes(t *testing.T) {
	oldCPUInferenceGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldNAPGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldCPUInferenceGate
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldNAPGate
	}()
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false

	cpuInference := &InferenceSpec{
		Template: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "cpu",
					Image: "example.com/cpu:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
					},
				}},
			},
		},
	}

	tests := []struct {
		name         string
		provider     string
		instanceType string
	}{
		{name: "aws cpu instance type", provider: consts.AWSCloudName, instanceType: "m6i.2xlarge"},
		{name: "azure e-series cpu instance type", provider: consts.AzureCloudName, instanceType: "Standard_E8s_v5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tt.provider)
			resourceSpec := &ResourceSpec{
				InstanceType:  tt.instanceType,
				Count:         pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			}
			if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err != nil {
				t.Fatalf("expected cpu instance type %s to pass validation, got %v", tt.instanceType, err)
			}
		})
	}

	t.Run("aws gpu family rejected for cpu path", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
		for _, instanceType := range []string{"g5.xlarge", "gr6.4xlarge"} {
			resourceSpec := &ResourceSpec{
				InstanceType:  instanceType,
				Count:         pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			}
			if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
				t.Fatalf("expected aws gpu instance family %s to be rejected for cpu path", instanceType)
			}
		}
	})

	t.Run("azure rejects non-azure cpu sku strings", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
		resourceSpec := &ResourceSpec{
			InstanceType:  "m6i.2xlarge",
			Count:         pointerToInt(1),
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
		}
		if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
			t.Fatalf("expected aws-style instance type to be rejected when CLOUD_PROVIDER=azure")
		}
	})

	armCases := []struct {
		name         string
		provider     string
		instanceType string
	}{
		{name: "aws arm cpu instance type rejected for vllm", provider: consts.AWSCloudName, instanceType: "m6g.2xlarge"},
		{name: "aws arm cpu local-nvme variant rejected for vllm", provider: consts.AWSCloudName, instanceType: "m6gd.2xlarge"},
		{name: "aws arm cpu network-optimized variant rejected for vllm", provider: consts.AWSCloudName, instanceType: "c7gn.2xlarge"},
		{name: "azure arm cpu instance type rejected for vllm", provider: consts.AzureCloudName, instanceType: "Standard_D16ps_v5"},
	}

	for _, tt := range armCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tt.provider)
			resourceSpec := &ResourceSpec{
				InstanceType:  tt.instanceType,
				Count:         pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			}
			if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
				t.Fatalf("expected arm cpu instance type %s to be rejected for vllm cpu path", tt.instanceType)
			}
		})
	}

	t.Run("cpu nap path still rejects partitioned workloads", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
		resourceSpec := &ResourceSpec{
			InstanceType: "Standard_D8s_v5",
			Count:        pointerToInt(1),
			Partition:    &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"kaito.sh/cpu-inference": "true",
			}},
		}
		if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
			t.Fatalf("expected partitioned cpu nap workload to be rejected")
		}
	})
}
