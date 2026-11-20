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

package inference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerImage(t *testing.T) {
	tests := []struct {
		name   string
		ss     *appsv1.StatefulSet
		expect string
	}{
		{
			name: "returns named container image matching statefulset name",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-workspace"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "sidecar", Image: "sidecar:v1"},
								{Name: "my-workspace", Image: "inference:v2"},
							},
						},
					},
				},
			},
			expect: "inference:v2",
		},
		{
			name: "falls back to first container when no name matches",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-workspace"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "other", Image: "image-a:v1"},
								{Name: "another", Image: "image-b:v2"},
							},
						},
					},
				},
			},
			expect: "image-a:v1",
		},
		{
			name: "empty containers returns empty string",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-workspace"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{},
						},
					},
				},
			},
			expect: "",
		},
		{
			name: "named container is not first in list",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "ws-abc"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "http-proxy", Image: "proxy:latest"},
								{Name: "metrics", Image: "metrics:v1"},
								{Name: "ws-abc", Image: "base:0.3.2"},
							},
						},
					},
				},
			},
			expect: "base:0.3.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainerImage(tt.ss)
			assert.Equal(t, tt.expect, got)
		})
	}
}
