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

package utils

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureKindExists checks if a specific GroupVersionKind (GVK) exists in the cluster.
func EnsureKindExists(restConfig *rest.Config, gvk schema.GroupVersionKind) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return false, err
	}

	resources, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if client.IgnoreNotFound(err) != nil {
		return false, err
	}

	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			return true, nil
		}
	}

	return false, nil
}
