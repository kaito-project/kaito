// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package featuregates

import (
	"errors"
	"fmt"

	"github.com/kaito-project/kaito/pkg/utils/consts"
	cliflag "k8s.io/component-base/cli/flag"
)

var (
	// FeatureGates is a map that holds	the feature gates and their default values for Kaito.
	FeatureGates = map[string]bool{
		consts.FeatureFlagVLLM: true,
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
