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

package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

func TestCacheConfigMapRequests(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kaitov1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	workspaces := []runtime.Object{
		&kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
			Name: "matching", Namespace: "tenant-a",
			Annotations: map[string]string{cache.ConfigAnnotation: "cache-attachment"},
		}},
		&kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
			Name: "different-config", Namespace: "tenant-a",
			Annotations: map[string]string{cache.ConfigAnnotation: "other"},
		}},
		&kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
			Name: "different-namespace", Namespace: "tenant-b",
			Annotations: map[string]string{cache.ConfigAnnotation: "cache-attachment"},
		}},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(workspaces...).Build()
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "cache-attachment", Namespace: "tenant-a",
	}}

	requests := cacheConfigMapRequests(context.Background(), kubeClient, configMap)
	if len(requests) != 1 {
		t.Fatalf("cacheConfigMapRequests() returned %d requests, want 1: %v", len(requests), requests)
	}
	if requests[0].Namespace != "tenant-a" || requests[0].Name != "matching" {
		t.Fatalf("cacheConfigMapRequests() = %v, want tenant-a/matching", requests[0])
	}
}
