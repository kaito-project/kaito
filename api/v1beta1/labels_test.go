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

package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetCompileCachePath(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		annotations map[string]string
		want        string
	}{
		{
			name:      "annotation absent",
			namespace: "team-a",
			want:      "",
		},
		{
			name:        "annotation false",
			namespace:   "team-a",
			annotations: map[string]string{AnnotationPersistCompileCache: "false"},
			want:        "",
		},
		{
			name:        "annotation true scopes by namespace",
			namespace:   "team-a",
			annotations: map[string]string{AnnotationPersistCompileCache: "true"},
			want:        "/opt/kaito/vllm-cache/team-a",
		},
		{
			name:        "empty namespace disables",
			annotations: map[string]string{AnnotationPersistCompileCache: "true"},
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &Workspace{ObjectMeta: metav1.ObjectMeta{Namespace: tt.namespace, Annotations: tt.annotations}}
			if got := GetCompileCachePath(ws); got != tt.want {
				t.Errorf("GetCompileCachePath() = %q, want %q", got, tt.want)
			}
		})
	}

	if PersistCompileCache(nil) {
		t.Errorf("PersistCompileCache(nil) = true, want false")
	}
}
