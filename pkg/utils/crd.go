// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	gvrCache = make(map[string]*metav1.APIResourceList)
)

// EnsureKindExists checks if a specific GroupVersionKind (GVK) exists in the cluster.
func EnsureKindExists(restConfig *rest.Config, gvk schema.GroupVersionKind) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return false, err
	}

	var resources *metav1.APIResourceList
	if cachedResource, ok := gvrCache[gvk.GroupVersion().String()]; ok {
		resources = cachedResource
	} else {
		resources, err = discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
		if client.IgnoreNotFound(err) != nil {
			return false, err
		}
		gvrCache[gvk.GroupVersion().String()] = resources
	}

	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			return true, nil
		}
	}

	return false, nil
}
