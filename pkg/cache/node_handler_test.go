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

package cache

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsNodeRelevant(t *testing.T) {
	t.Run("ready node returns true", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		if !isNodeRelevant(node) {
			t.Fatal("expected ready node to be relevant")
		}
	})

	t.Run("non-node object returns false", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a"}}
		if isNodeRelevant(pod) {
			t.Fatal("expected non-node object to be irrelevant")
		}
	})
}
