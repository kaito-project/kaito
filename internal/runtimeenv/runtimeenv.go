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

// Package runtimeenv builds the api/env.Env from the controller's parsed
// flags, feature gates, and process environment.
package runtimeenv

import (
	"os"

	"k8s.io/klog/v2"

	"github.com/kaito-project/kaito/api/env"
	"github.com/kaito-project/kaito/internal/featuregates"
	"github.com/kaito-project/kaito/internal/k8s/k8sclient"
	"github.com/kaito-project/kaito/pkg/sku"
)

// New returns an Env built from the parsed feature gates and node provisioner.
// Call it after flags are parsed; set Client once the manager exists.
func New(skuHandler sku.CloudSKUHandler) *env.Env {
	releaseNamespace, err := k8sclient.GetReleaseNamespace()
	if err != nil {
		klog.ErrorS(err, "unable to determine release namespace")
	}
	return &env.Env{
		Features: env.Features{
			ModelStreaming:    featuregates.FeatureGates[featuregates.FeatureFlagModelStreaming],
			EnableMIG:         featuregates.FeatureGates[featuregates.FeatureFlagEnableMIG],
			EnableAccelerator: featuregates.FeatureGates[featuregates.FeatureFlagEnableAccelerator],
		},
		NodeProvisioner:     env.NodeProvisioner(featuregates.NodeProvisioner),
		NodeClassNames:      featuregates.AllowedNodeClassNames,
		CloudProvider:       env.CloudProvider(os.Getenv("CLOUD_PROVIDER")),
		ReleaseNamespace:    releaseNamespace,
		ReleaseNamespaceErr: err,
		SKUHandler:          skuHandler,
	}
}
