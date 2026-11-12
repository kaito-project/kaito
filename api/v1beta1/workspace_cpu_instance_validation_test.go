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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/consts"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
)

func TestValidateCreateWithInference_AllowsCPUNAPInstanceTypes(t *testing.T) {
	RegisterValidationTestModels()
	oldNAPGate := featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = oldNAPGate
	}()
	featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = false

	cpuInference := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-validation"}}}

	tests := []struct {
		name         string
		provider     string
		instanceType string
	}{
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

	unsupportedProviderCases := []struct {
		name         string
		provider     string
		instanceType string
	}{
		{name: "aws cpu instance type unsupported", provider: consts.AWSCloudName, instanceType: "m6i.2xlarge"},
		{name: "aws arm cpu instance type unsupported provider", provider: consts.AWSCloudName, instanceType: "m6g.2xlarge"},
		{name: "aws arm a1 cpu instance type unsupported provider", provider: consts.AWSCloudName, instanceType: "a1.4xlarge"},
		{name: "aws arm cpu local-nvme variant unsupported provider", provider: consts.AWSCloudName, instanceType: "m6gd.2xlarge"},
		{name: "aws arm cpu network-optimized variant unsupported provider", provider: consts.AWSCloudName, instanceType: "c7gn.2xlarge"},
	}

	for _, tt := range unsupportedProviderCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tt.provider)
			resourceSpec := &ResourceSpec{
				InstanceType:  tt.instanceType,
				Count:         pointerToInt(1),
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			}
			if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
				t.Fatalf("expected AWS CPU instance type %s to be rejected for CPU inference", tt.instanceType)
			}
		})
	}

	t.Run("cpu nap path rejects conflicting sku selector", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
		resourceSpec := &ResourceSpec{
			InstanceType: "Standard_D8s_v5",
			Count:        pointerToInt(1),
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"kaito.sh/cpu-inference": "true",
				LabelSKU:                 "pool-a",
			}},
		}
		if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
			t.Fatalf("expected conflicting sku selector to be rejected for cpu nap workload")
		}
	})

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

	t.Run("cpu byo path still rejects partitioned workloads", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
		oldDisableNAP := featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning]
		defer func() {
			featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = oldDisableNAP
		}()
		featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = true

		resourceSpec := &ResourceSpec{
			Count:     pointerToInt(1),
			Partition: &PartitionSpec{Mode: PartitionModeAccelerator, Count: pointerToInt(2)},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"kaito.sh/cpu-inference": "true",
			}},
		}
		if err := resourceSpec.validateCreateWithInference(context.TODO(), cpuInference, false, model.RuntimeNameVLLM, ""); err == nil {
			t.Fatalf("expected partitioned cpu byo workload to be rejected")
		}
	})
}
