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

package inferenceset

import (
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// clientObjectSpecEqual compares the spec field of two client.Objects for equality.
// For example:
//
//	a:   {"apiVersion": "apps/v1", "kind": "Deployment", "spec": {"replicas": 2}}
//	b:   {"apiVersion": "apps/v1", "kind": "Deployment", "spec": {"replicas": 2}}
//	result: true
//
//	c:   {"apiVersion": "apps/v1", "kind": "Deployment", "spec": {"replicas": 3}}
//	d:   {"apiVersion": "apps/v1", "kind": "Deployment", "spec": {"replicas": 2}}
//	result: false
func clientObjectSpecEqual(a, b client.Object) (bool, error) {
	aUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(a)
	if err != nil {
		return false, err
	}
	bUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(b)
	if err != nil {
		return false, err
	}
	aSpec, aOK, err := unstructured.NestedMap(aUnstructured, "spec")
	if err != nil {
		return false, err
	}
	bSpec, bOK, err := unstructured.NestedMap(bUnstructured, "spec")
	if err != nil {
		return false, err
	}
	return aOK && bOK && equality.Semantic.DeepEqual(aSpec, bSpec), nil
}
