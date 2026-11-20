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

package admission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	_ "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/admissionregistration/v1/validatingwebhookconfiguration/fake"
	_ "knative.dev/pkg/injection/clients/namespacedkube/informers/core/v1/secret/fake"
	reconcilertesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/resourcesemantics"

	"github.com/kaito-project/kaito/api/env"
)

// probe records the Env its Validate call sees.
type probe struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

var seen *env.Env

func (p *probe) DeepCopyObject() runtime.Object { c := *p; return &c }
func (p *probe) SetDefaults(context.Context)    {}
func (p *probe) Validate(ctx context.Context) *apis.FieldError {
	seen = env.FromContext(ctx)
	return nil
}

func TestValidationControllerInjectsEnv(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "kaito.sh", Version: "v1", Kind: "Probe"}
	ctx, _ := reconcilertesting.SetupFakeContext(t)
	ctx = webhook.WithOptions(ctx, webhook.Options{SecretName: "webhook-secret"})
	e := &env.Env{CloudProvider: "azure"}

	impl := NewValidationController(e, "validation.probe.kaito.sh", "/validate/probe",
		map[schema.GroupVersionKind]resourcesemantics.GenericCRD{gvk: &probe{}})(ctx, nil)
	ac, ok := impl.Reconciler.(webhook.AdmissionController)
	require.True(t, ok)

	seen = nil
	resp := ac.Admit(ctx, &admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Kind:      metav1.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind},
		Object:    runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"p"}}`)},
	})
	require.True(t, resp.Allowed, "%v", resp.Result)
	assert.Same(t, e, seen)
}
