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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestGetControllerKeyForWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		workspace *v1beta1.Workspace
		expected  *client.ObjectKey
	}{
		{
			name: "Workspace with valid label",
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Labels: map[string]string{
						consts.WorkspaceCreatedByInferenceSetLabel: "test-name",
					},
				},
			},
			expected: &client.ObjectKey{Namespace: "test-namespace", Name: "test-name"},
		},
		{
			name: "Workspace without label",
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Labels:    map[string]string{},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getControllerKeyForWorkspace(tt.workspace)
			assert.Equal(t, tt.expected, result)
		})
	}
}
