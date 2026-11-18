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

package nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestSelectNodes(t *testing.T) {
	makeNode := func(name string, labels map[string]string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}

	t.Run("preferred node ranked first", func(t *testing.T) {
		nodes := []*corev1.Node{
			makeNode("node-c", nil),
			makeNode("node-b", nil),
			makeNode("node-a", nil),
		}
		result := SelectNodes(nodes, []string{"node-b"}, nil, 3)
		assert.Len(t, result, 3)
		assert.Equal(t, "node-b", result[0].Name)
	})

	t.Run("count limits results", func(t *testing.T) {
		nodes := []*corev1.Node{
			makeNode("node-1", nil),
			makeNode("node-2", nil),
			makeNode("node-3", nil),
		}
		result := SelectNodes(nodes, nil, nil, 2)
		assert.Len(t, result, 2)
	})

	t.Run("returns all when count exceeds available", func(t *testing.T) {
		nodes := []*corev1.Node{makeNode("node-1", nil)}
		result := SelectNodes(nodes, nil, nil, 5)
		assert.Len(t, result, 1)
	})

	t.Run("previous node ranked above provisioner node", func(t *testing.T) {
		nodes := []*corev1.Node{
			makeNode("node-prov", map[string]string{labelGPUProvisionerCustom: "gpu"}),
			makeNode("node-prev", nil),
		}
		result := SelectNodes(nodes, nil, []string{"node-prev"}, 2)
		assert.Len(t, result, 2)
		assert.Equal(t, "node-prev", result[0].Name)
	})

	t.Run("karpenter node ranked above plain node", func(t *testing.T) {
		nodes := []*corev1.Node{
			makeNode("plain", nil),
			makeNode("karpenter", map[string]string{karpenterv1.NodePoolLabelKey: "default"}),
		}
		result := SelectNodes(nodes, nil, nil, 2)
		assert.Len(t, result, 2)
		assert.Equal(t, "karpenter", result[0].Name)
	})

	t.Run("alphabetical fallback for equal priority", func(t *testing.T) {
		nodes := []*corev1.Node{
			makeNode("node-z", nil),
			makeNode("node-a", nil),
		}
		result := SelectNodes(nodes, nil, nil, 2)
		assert.Len(t, result, 2)
		assert.Equal(t, "node-a", result[0].Name)
	})
}
