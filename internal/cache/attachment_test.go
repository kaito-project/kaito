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
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestResolveAttachment(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "workspace",
			Namespace:   "inference",
			Annotations: map[string]string{ConfigAnnotation: "cache-attachment"},
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-attachment", Namespace: "inference"},
		Data: map[string]string{ConfigKey: `
modelWeights:
  provider: dacs
  parameters:
    cacheName: shared
kvCache:
  provider: flexkv
  mode: Required
`},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	attachment, err := ResolveAttachment(context.Background(), kubeClient, workspace)
	if err != nil {
		t.Fatalf("ResolveAttachment() error = %v", err)
	}
	if attachment.ModelWeights.Mode != ModeOpportunistic {
		t.Fatalf("default modelWeights mode = %q, want %q", attachment.ModelWeights.Mode, ModeOpportunistic)
	}
	if attachment.ModelWeights.Parameters["cacheName"] != "shared" {
		t.Fatalf("cacheName = %q, want shared", attachment.ModelWeights.Parameters["cacheName"])
	}
	if attachment.KVCache.Mode != ModeRequired {
		t.Fatalf("kvCache mode = %q, want %q", attachment.KVCache.Mode, ModeRequired)
	}
}

func TestDecodeAttachmentRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "unknown field",
			data:    "modelWeights:\n  provider: dacs\n  unknown: value\n",
			wantErr: "field unknown not found",
		},
		{
			name:    "missing provider",
			data:    "modelWeights:\n  mode: Required\n",
			wantErr: "modelWeights.provider is required",
		},
		{
			name:    "invalid mode",
			data:    "modelWeights:\n  provider: dacs\n  mode: BestEffort\n",
			wantErr: "modelWeights.mode",
		},
		{
			name:    "empty attachment",
			data:    "{}\n",
			wantErr: "at least one",
		},
		{
			name:    "multiple documents",
			data:    "modelWeights:\n  provider: dacs\n---\nkvCache:\n  provider: flexkv\n",
			wantErr: "multiple YAML documents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeAttachment([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("decodeAttachment() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveAttachmentUsesWorkspaceNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	workspace := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
		Namespace: "tenant-a",
		Annotations: map[string]string{
			ConfigAnnotation: "cache-attachment",
		},
	}}
	otherNamespace := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-attachment", Namespace: "tenant-b"},
		Data:       map[string]string{ConfigKey: "modelWeights:\n  provider: dacs\n"},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(otherNamespace).Build()

	_, err := ResolveAttachment(context.Background(), kubeClient, workspace)
	if err == nil || !strings.Contains(err.Error(), "tenant-a/cache-attachment") {
		t.Fatalf("ResolveAttachment() error = %v, want same-namespace lookup failure", err)
	}
}

func TestResolveEffectiveAttachmentModeOnInvalidContent(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name      string
		mode      string
		wantError bool
	}{
		{name: "opportunistic falls back", mode: "Opportunistic"},
		{name: "omitted mode defaults to opportunistic fallback"},
		{name: "required blocks", mode: "Required", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workspace := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
				Name: "workspace", Namespace: "inference",
				Annotations: map[string]string{ConfigAnnotation: "cache-attachment"},
			}}
			modeLine := ""
			if tt.mode != "" {
				modeLine = "  mode: " + tt.mode + "\n"
			}
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cache-attachment", Namespace: "inference"},
				Data: map[string]string{ConfigKey: "modelWeights:\n  provider: dacs\n" +
					modeLine + "  unknown: value\n"},
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

			attachment, err := ResolveEffectiveAttachment(context.Background(), kubeClient, workspace)
			if tt.wantError {
				if err == nil {
					t.Fatal("ResolveEffectiveAttachment() error = nil, want blocking error")
				}
				return
			}
			if err != nil || attachment != nil {
				t.Fatalf("ResolveEffectiveAttachment() = (%v, %v), want (nil, nil)", attachment, err)
			}
		})
	}
}
