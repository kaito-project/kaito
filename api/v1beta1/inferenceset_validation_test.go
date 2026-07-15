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

	"github.com/kaito-project/kaito/pkg/featuregates"
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

// setMIGGates enables/disables the MIG and BYO feature gates for the duration of
// a test and restores the previous values on cleanup.
func setMIGGates(t *testing.T, enableMIG, napDisabled bool) {
	t.Helper()
	origMIG := featuregates.FeatureGates[consts.FeatureFlagEnableMIG]
	origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = enableMIG
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = napDisabled
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = origMIG
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
	})
}

func TestValidateInferenceSetMIG(t *testing.T) {
	tests := []struct {
		name       string
		enableMIG  bool
		napDisable bool
		resource   InferenceSetResourceSpec
		errContent string
	}{
		{
			name:     "nil partition passes",
			resource: InferenceSetResourceSpec{},
		},
		{
			name:       "valid MIG",
			enableMIG:  true,
			napDisable: true,
			resource:   InferenceSetResourceSpec{Partition: &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"}},
		},
		{
			name:       "unsupported mode",
			enableMIG:  true,
			napDisable: true,
			resource:   InferenceSetResourceSpec{Partition: &PartitionSpec{Mode: "bogus", Profile: "1g.10gb"}},
			errContent: "unsupported partition mode",
		},
		{
			name:       "feature gate disabled",
			enableMIG:  false,
			napDisable: true,
			resource:   InferenceSetResourceSpec{Partition: &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"}},
			errContent: "MIG support is not enabled",
		},
		{
			name:       "invalid profile",
			enableMIG:  true,
			napDisable: true,
			resource:   InferenceSetResourceSpec{Partition: &PartitionSpec{Mode: PartitionModeMIG, Profile: "bogus"}},
			errContent: "invalid MIG profile",
		},
		{
			name:       "NAP not disabled",
			enableMIG:  true,
			napDisable: false,
			resource:   InferenceSetResourceSpec{Partition: &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"}},
			errContent: "only supported with BYO nodes",
		},
		{
			name:       "instanceType set with partition",
			enableMIG:  true,
			napDisable: true,
			resource:   InferenceSetResourceSpec{InstanceType: "Standard_NC24ads_A100_v4", Partition: &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"}},
			errContent: "instanceType must be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMIGGates(t, tt.enableMIG, tt.napDisable)
			errs := validateInferenceSetPartition(&tt.resource)
			if tt.errContent == "" {
				assert.Nil(t, errs)
			} else {
				assert.NotNil(t, errs)
				assert.Contains(t, errs.Error(), tt.errContent)
			}
		})
	}
}

func TestInferenceSetMIGImmutable(t *testing.T) {
	setMIGGates(t, true, true)
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
