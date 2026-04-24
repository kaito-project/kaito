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

package nodeclass

import (
	"context"
	"errors"
	"testing"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	awsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGenerateEC2NodeClassManifest(t *testing.T) {
	t.Run("Should generate a valid EC2NodeClass object", func(t *testing.T) {
		t.Setenv("CLUSTER_NAME", "test-cluster")

		nodeClass := GenerateEC2NodeClassManifest(context.Background())

		assert.NotNil(t, nodeClass)
		assert.Equal(t, consts.NodeClassName, nodeClass.Name)
		assert.Equal(t, awsv1.AMIFamilyAL2, *nodeClass.Spec.AMIFamily)
		assert.Equal(t, "KarpenterNodeRole-test-cluster", nodeClass.Spec.Role)
		assert.Equal(t, "test-cluster", nodeClass.Spec.SubnetSelectorTerms[0].Tags["karpenter.sh/discovery"])
		assert.Equal(t, "test-cluster", nodeClass.Spec.SecurityGroupSelectorTerms[0].Tags["karpenter.sh/discovery"])
	})
}

func TestEnsureGlobalAKSNodeClasses(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*test.MockClient)
		expectErr  bool
	}{
		{
			name: "creates both AKSNodeClasses when not found",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.azure.com", Resource: "aksnodeclasses"}, "")).
					Times(2)
				m.On("Create", mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(nil).
					Times(2)
			},
			expectErr: false,
		},
		{
			name: "skips creation when both already exist",
			setupMocks: func(m *test.MockClient) {
				ubuntuNC := &azurev1beta1.AKSNodeClass{
					ObjectMeta: metav1.ObjectMeta{Name: consts.AKSNodeClassUbuntuName},
				}
				azureLinuxNC := &azurev1beta1.AKSNodeClass{
					ObjectMeta: metav1.ObjectMeta{Name: consts.AKSNodeClassAzureLinuxName},
				}
				m.CreateOrUpdateObjectInMap(ubuntuNC)
				m.CreateOrUpdateObjectInMap(azureLinuxNC)
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(nil).
					Times(2)
			},
			expectErr: false,
		},
		{
			name: "returns error on Get failure (non-NotFound)",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(errors.New("server error")).
					Times(1)
			},
			expectErr: true,
		},
		{
			name: "handles AlreadyExists on Create gracefully",
			setupMocks: func(m *test.MockClient) {
				m.On("Get", mock.Anything, mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.azure.com", Resource: "aksnodeclasses"}, "")).
					Times(2)
				m.On("Create", mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).
					Return(apierrors.NewAlreadyExists(schema.GroupResource{Group: "karpenter.azure.com", Resource: "aksnodeclasses"}, "")).
					Times(2)
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.setupMocks(mockClient)

			err := EnsureGlobalAKSNodeClasses(context.Background(), mockClient)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			mockClient.AssertExpectations(t)
		})
	}
}
