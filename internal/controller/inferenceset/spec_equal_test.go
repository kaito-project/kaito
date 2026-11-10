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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClientObjectSpecEqual(t *testing.T) {
	i32 := func(n int32) *int32 { return &n }

	tests := []struct {
		name string
		a, b client.Object
		want bool
	}{
		{
			name: "equal deployment specs despite different metadata",
			a: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns1"},
				Spec: appsv1.DeploymentSpec{
					Replicas: i32(2),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c1", Image: "busybox:latest"}}},
					},
				},
			},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns2", Labels: map[string]string{"x": "y"}},
				Spec: appsv1.DeploymentSpec{
					Replicas: i32(2),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c1", Image: "busybox:latest"}}},
					},
				},
			},
			want: true,
		},
		{
			name: "different deployment specs (replicas)",
			a: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
				Spec:       appsv1.DeploymentSpec{Replicas: i32(2)},
			},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"},
				Spec:       appsv1.DeploymentSpec{Replicas: i32(3)},
			},
			want: false,
		},
		{
			name: "equal service specs with different names",
			a: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
					Selector: map[string]string{"app": "demo"},
				},
			},
			b: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
					Selector: map[string]string{"app": "demo"},
				},
			},
			want: true,
		},
		{
			name: "one object without spec (ConfigMap) vs Service",
			a:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"}},
			b: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
			want: false,
		},
		{
			name: "both objects without spec (ConfigMap vs ConfigMap)",
			a:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-a", Namespace: "ns"}},
			b:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-b", Namespace: "ns"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := clientObjectSpecEqual(tt.a, tt.b)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
