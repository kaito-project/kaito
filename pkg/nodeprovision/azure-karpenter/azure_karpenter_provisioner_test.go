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

package azurekarpenter

import (
	"context"
	"errors"
	"testing"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestAzureKarpenterProvisionerImplementsInterface(t *testing.T) {
	mockClient := test.NewClient()
	var _ nodeprovision.NodeProvisioner = NewAzureKarpenterProvisioner(mockClient)
}

func TestName(t *testing.T) {
	p := NewAzureKarpenterProvisioner(test.NewClient())
	assert.Equal(t, "AzureKarpenterProvisioner", p.Name())
}

func TestVerifyRequiredCRDs(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*test.MockClient)
		expectErr  bool
		errMsg     string
	}{
		{
			name: "all CRDs exist",
			setupMocks: func(m *test.MockClient) {
				for _, crdName := range requiredCRDs {
					crd := &apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{Name: crdName},
					}
					m.CreateOrUpdateObjectInMap(crd)
				}
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(nil)
			},
			expectErr: false,
		},
		{
			name: "first CRD not found",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "customresourcedefinitions"}, requiredCRDs[0])).
					Once()
			},
			expectErr: true,
			errMsg:    "required CRD",
		},
		{
			name: "API server error",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(errors.New("connection refused")).
					Once()
			},
			expectErr: true,
			errMsg:    "failed to check CRD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.setupMocks(mockClient)

			err := verifyRequiredCRDs(context.Background(), mockClient)

			if tc.expectErr {
				assert.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStart(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*test.MockClient)
		expectErr  bool
		errMsg     string
	}{
		{
			name: "succeeds when CRDs exist and nodeclass creation succeeds",
			setupMocks: func(m *test.MockClient) {
				// verifyRequiredCRDs: all CRDs exist
				for _, crdName := range requiredCRDs {
					crd := &apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{Name: crdName},
					}
					m.CreateOrUpdateObjectInMap(crd)
				}
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(nil)
				// EnsureGlobalAKSNodeClasses: both not found, create succeeds
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.azure.com", Resource: "aksnodeclasses"}, ""))
				m.On("Create", mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(nil)
			},
			expectErr: false,
		},
		{
			name: "fails when CRDs do not exist",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "customresourcedefinitions"}, "")).
					Once()
			},
			expectErr: true,
			errMsg:    "required CRD",
		},
		{
			name: "fails when nodeclass creation fails",
			setupMocks: func(m *test.MockClient) {
				// CRDs exist
				for _, crdName := range requiredCRDs {
					crd := &apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{Name: crdName},
					}
					m.CreateOrUpdateObjectInMap(crd)
				}
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&apiextensionsv1.CustomResourceDefinition{}), mock.Anything).
					Return(nil)
				// EnsureGlobalAKSNodeClasses: Get fails
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.azure.com", Resource: "aksnodeclasses"}, ""))
				m.On("Create", mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(errors.New("create failed")).
					Once()
			},
			expectErr: true,
			errMsg:    "failed to bootstrap global AKSNodeClasses",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.setupMocks(mockClient)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			p := NewAzureKarpenterProvisioner(mockClient)
			err := p.Start(ctx)

			if tc.expectErr {
				assert.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				assert.NoError(t, err)
				// Cancel context to stop the background goroutine.
				cancel()
			}
		})
	}
}
