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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	v1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestRuntimeUpgradeAuthorizationUsesSelectedImageTag(t *testing.T) {
	makeSTS := func(image string) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "ws"}, Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ws", Image: image}}}}}}
	}
	for _, tc := range []struct {
		name, label, desired, existing string
		want                           bool
	}{
		{"CPU current authorization", "0.0.2", "registry:5000/kaito-base-vllm-cpu:0.0.2", "registry:5000/kaito-base-vllm-cpu:old", true},
		{"GPU tag cannot authorize CPU", "0.0.1", "registry:5000/kaito-base-vllm-cpu:0.0.2", "registry:5000/kaito-base-vllm-cpu:old", false},
		{"old CPU authorization cannot approve next release", "0.0.2", "registry:5000/kaito-base-vllm-cpu:0.0.3", "registry:5000/kaito-base-vllm-cpu:0.0.2", false},
		{"next CPU authorization", "0.0.3", "registry:5000/kaito-base-vllm-cpu:0.0.3", "registry:5000/kaito-base-vllm-cpu:0.0.2", true},
		{"GPU behavior unchanged", "0.0.1", "registry:5000/kaito-base:0.0.1", "registry:5000/kaito-base:old", true},
		{"no authorization", "", "registry:5000/kaito-base:0.0.1", "registry:5000/kaito-base:old", false},
		{"image already current", "0.0.2", "registry:5000/kaito-base-vllm-cpu:0.0.2", "registry:5000/kaito-base-vllm-cpu:0.0.2", false},
		{"untagged", "latest", "registry:5000/kaito-base-vllm-cpu", "registry:5000/kaito-base-vllm-cpu:old", false},
		{"digest only", "0.0.2", "registry:5000/kaito-base-vllm-cpu@sha256:" + strings.Repeat("a", 64), "registry:5000/kaito-base-vllm-cpu:old", false},
		{"tag plus digest", "0.0.2", "registry:5000/kaito-base-vllm-cpu:0.0.2@sha256:" + strings.Repeat("a", 64), "registry:5000/kaito-base-vllm-cpu:old", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{v1alpha1.LabelUpgradeToVersion: tc.label}}}
			require.Equal(t, tc.want, shouldUpgradeBaseImage(ws, makeSTS(tc.existing), makeSTS(tc.desired)))
		})
	}
}
