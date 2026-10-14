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
	"testing"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const testAttachmentDataAnnotation = "test.kaito.sh/cache-attachment-data"

func attachTestConfig(t *testing.T, workspace *kaitov1beta1.Workspace, attachment *Attachment) client.Client {
	t.Helper()
	if workspace.Namespace == "" {
		workspace.Namespace = "default"
	}
	if workspace.Annotations == nil {
		workspace.Annotations = make(map[string]string)
	}
	const configMapName = "cache-attachment"
	workspace.Annotations[ConfigAnnotation] = configMapName

	data, err := yaml.Marshal(attachment)
	if err != nil {
		t.Fatalf("marshal cache attachment: %v", err)
	}
	workspace.Annotations[testAttachmentDataAnnotation] = string(data)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: workspace.Namespace},
		Data:       map[string]string{ConfigKey: string(data)},
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to test scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
}

func workspaceWithTestAttachment(attachment *Attachment) *kaitov1beta1.Workspace {
	workspace := &kaitov1beta1.Workspace{}
	data, err := yaml.Marshal(attachment)
	if err != nil {
		panic(err)
	}
	workspace.Annotations = map[string]string{
		ConfigAnnotation:             "cache-attachment",
		testAttachmentDataAnnotation: string(data),
	}
	return workspace
}

func attachmentClientForWorkspace(t *testing.T, workspace *kaitov1beta1.Workspace) client.Client {
	t.Helper()
	attachment := attachmentForTestWorkspace(t, workspace)
	return attachTestConfig(t, workspace, attachment)
}

func attachmentForTestWorkspace(t *testing.T, workspace *kaitov1beta1.Workspace) *Attachment {
	t.Helper()
	raw := workspace.Annotations[testAttachmentDataAnnotation]
	attachment, err := decodeAttachment([]byte(raw))
	if err != nil {
		t.Fatalf("decode test cache attachment: %v", err)
	}
	return attachment
}

func resolveTestAttachment(t *testing.T, workspace *kaitov1beta1.Workspace) *Attachment {
	t.Helper()
	kubeClient := attachmentClientForWorkspace(t, workspace)
	attachment, err := ResolveAttachment(context.Background(), kubeClient, workspace)
	if err != nil {
		t.Fatalf("resolve test cache attachment: %v", err)
	}
	return attachment
}
