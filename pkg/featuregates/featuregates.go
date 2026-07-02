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
	"maps"

	cliflag "k8s.io/component-base/cli/flag"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// defaults captures the built-in default value for every known KAITO feature gate.
// Names not present here are rejected by ParseAndValidateFeatureGates.
var defaults = map[string]bool{
	consts.FeatureFlagVLLM:                               true,
	consts.FeatureFlagGatewayAPIInferenceExtension:       false,
	consts.FeatureFlagEnableInferenceSetController:       true,
	consts.FeatureFlagEnableMultiRoleInferenceController: false,
	consts.FeatureFlagModelMirror:                        false,
	consts.FeatureFlagModelStreaming:                     false,
	consts.FeatureFlagEnableBaseImageAutoUpgrade:         false,
	//	Add more feature gates here
}

// gates holds the resolved value for every known feature gate.
var gates = maps.Clone(defaults)

// Enabled reports whether the named feature gate is currently on.
// Unknown names return false.
func Enabled(name string) bool {
	return gates[name]
}

// Set overrides the value of a known feature gate. Unknown names are ignored
// so callers (tests, startup wiring) can be defensive.
func Set(name string, value bool) {
	if _, ok := defaults[name]; !ok {
		return
	}
	gates[name] = value
}

// ParseAndValidateFeatureGates parses the --feature-gates flag value and
// updates the internal gate state. Unknown gate names return an error.
func ParseAndValidateFeatureGates(featureGates string) error {
	gateMap := map[string]bool{}
	if err := cliflag.NewMapStringBool(&gateMap).Set(featureGates); err != nil {
		return err
	}
	if len(gateMap) == 0 {
		return nil
	}

	var invalidFeatures string
	for key, val := range gateMap {
		if _, ok := defaults[key]; !ok {
			invalidFeatures = fmt.Sprintf("%s, %s", invalidFeatures, key)
			continue
		}
		gates[key] = val
	}

	if invalidFeatures != "" {
		return errors.New("invalid feature gate(s) " + invalidFeatures)
	}

	return nil
}
