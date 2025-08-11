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

package featuregates

import (
	"errors"
	"fmt"

	cliflag "k8s.io/component-base/cli/flag"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

var (
	// FeatureGates is a map that holds the feature gate names and their default values for Kaito.
	FeatureGates = map[string]bool{
		consts.FeatureFlagVLLM:                         true,
		consts.FeatureFlagEnsureNodeClass:              false,
		consts.FeatureFlagDisableNodeAutoProvisioning:  false,
		consts.FeatureFlagGatewayAPIInferenceExtension: false,
		//	Add more feature gates here
	}
)

// ParseAndValidateFeatureGates parses the feature gates flag and sets the environment variables for each feature.
func ParseAndValidateFeatureGates(featureGates string) error {
	gateMap := map[string]bool{}
	if err := cliflag.NewMapStringBool(&gateMap).Set(featureGates); err != nil {
		return err
	}
	if len(gateMap) == 0 {
		// no feature gates set
		return nil
	}

	var invalidFeatures string
	for key, val := range gateMap {
		if _, ok := FeatureGates[key]; !ok {
			invalidFeatures = fmt.Sprintf("%s, %s", invalidFeatures, key)
			continue
		}
		FeatureGates[key] = val
	}

	if invalidFeatures != "" {
		return errors.New("invalid feature gate(s) " + invalidFeatures)
	}

	return nil
}
