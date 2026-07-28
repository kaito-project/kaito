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

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestInferenceSetValidate(t *testing.T) {
	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-is",
			Namespace: "default",
		},
		Spec: InferenceSetSpec{},
	}
	errs := is.Validate(context.Background())
	assert.Nil(t, errs)
}

func TestInferenceSetSetDefaults(t *testing.T) {
	is := &InferenceSet{}
	is.SetDefaults(context.Background())
}

func TestInferenceSetSupportedVerbs(t *testing.T) {
	is := &InferenceSet{}
	verbs := is.SupportedVerbs()
	assert.Len(t, verbs, 2)
}

func TestValidateInferenceSetMaintenanceWindow(t *testing.T) {
	// nil autoUpgrade
	errs := validateInferenceSetMaintenanceWindow(nil)
	assert.Nil(t, errs)

	// valid window
	errs = validateInferenceSetMaintenanceWindow(&AutoUpgradePolicy{
		MaintenanceWindow: &MaintenanceWindow{
			Schedule: "0 2 * * 6",
		},
	})
	assert.Nil(t, errs)
}

func TestInferenceSetBenchmarkHelpers(t *testing.T) {
	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}
	_ = GetInferenceSetRuntimeName(is)
	_ = IsInferenceSetBenchmarkEnabled(is)
	_ = ShouldRunInferenceSetBenchmark(is)
}

func TestInferenceSetMIGImmutable(t *testing.T) {
	makeIS := func(profile string) *InferenceSet {
		var p *PartitionSpec
		if profile != "" {
			p = &PartitionSpec{Mode: PartitionModeMIG, Profile: profile}
		}
		return &InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: InferenceSetSpec{
				Template: InferenceSetTemplate{
					Resource: InferenceSetResourceSpec{Partition: p},
				},
			},
		}
	}

	// Unchanged partition is allowed.
	errs := makeIS("1g.10gb").validateUpdate(makeIS("1g.10gb"))
	assert.Nil(t, errs)

	// Changing the profile is rejected.
	errs = makeIS("2g.20gb").validateUpdate(makeIS("1g.10gb"))
	assert.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "field is immutable")

	// Adding a partition to a non-partitioned set is rejected.
	errs = makeIS("1g.10gb").validateUpdate(makeIS(""))
	assert.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "field is immutable")
}

func TestInferenceSet_validateInstanceType(t *testing.T) {
	tests := []struct {
		name            string
		nodeProvisioner string
		instanceType    string
		wantErr         bool
		errField        string
	}{
		{
			name:            "BYO mode with empty instanceType - valid",
			nodeProvisioner: consts.NodeProvisionerBYO,
			instanceType:    "",
			wantErr:         false,
		},
		{
			name:            "BYO mode with instanceType set - invalid",
			nodeProvisioner: consts.NodeProvisionerBYO,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "Karpenter mode with instanceType set - valid",
			nodeProvisioner: consts.NodeProvisionerKarpenter,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "Karpenter mode with empty instanceType - invalid",
			nodeProvisioner: consts.NodeProvisionerKarpenter,
			instanceType:    "",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "AzureGPU mode with instanceType set - valid",
			nodeProvisioner: consts.NodeProvisionerAzureGPU,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "AzureGPU mode with empty instanceType - invalid",
			nodeProvisioner: consts.NodeProvisionerAzureGPU,
			instanceType:    "",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "Unknown provisioner with instanceType - no error",
			nodeProvisioner: "",
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "Unknown provisioner without instanceType - no error",
			nodeProvisioner: "",
			instanceType:    "",
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := consts.ActiveNodeProvisioner
			consts.ActiveNodeProvisioner = tt.nodeProvisioner
			defer func() { consts.ActiveNodeProvisioner = orig }()

			is := &InferenceSet{
				Spec: InferenceSetSpec{
					Template: InferenceSetTemplate{
						Resource: InferenceSetResourceSpec{
							InstanceType: tt.instanceType,
						},
					},
				},
			}
			err := is.validateInstanceType()
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
