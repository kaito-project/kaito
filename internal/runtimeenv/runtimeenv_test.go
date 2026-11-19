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

package runtimeenv

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/api/env"
	"github.com/kaito-project/kaito/internal/featuregates"
)

func TestNew(t *testing.T) {
	origMIG := featuregates.FeatureGates[featuregates.FeatureFlagEnableMIG]
	origProvisioner := featuregates.NodeProvisioner
	t.Cleanup(func() {
		featuregates.FeatureGates[featuregates.FeatureFlagEnableMIG] = origMIG
		featuregates.NodeProvisioner = origProvisioner
	})
	featuregates.FeatureGates[featuregates.FeatureFlagEnableMIG] = true
	featuregates.NodeProvisioner = featuregates.NodeProvisionerKarpenter
	t.Setenv("CLOUD_PROVIDER", "azure")
	t.Setenv("RELEASE_NAMESPACE", "kaito-system")

	e := New(nil)

	assert.Equal(t, env.Features{EnableMIG: true}, e.Features)
	assert.Equal(t, env.NodeProvisionerKarpenter, e.NodeProvisioner)
	assert.Equal(t, env.CloudProvider("azure"), e.CloudProvider)
	assert.Equal(t, "kaito-system", e.ReleaseNamespace)
	assert.NotNil(t, e.NodeClassNames)
}
