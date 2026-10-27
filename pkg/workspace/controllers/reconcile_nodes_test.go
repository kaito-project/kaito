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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
)

// nodesReadyProvisioner reports a fixed EnsureNodesReady result.
type nodesReadyProvisioner struct {
	nodeprovision.NodeProvisioner
	ready, needRequeue bool
}

func (p *nodesReadyProvisioner) ProvisionNodes(context.Context, *v1beta1.Workspace) error { return nil }

func (p *nodesReadyProvisioner) EnsureNodesReady(context.Context, *v1beta1.Workspace) (bool, bool, error) {
	return p.ready, p.needRequeue, nil
}

func TestReconcileNodesRequeue(t *testing.T) {
	testcases := map[string]struct {
		ready, needRequeue bool
		expected           *reconcile.Result
	}{
		"ready":                    {ready: true},
		"waiting for node plugins": {needRequeue: true, expected: &reconcile.Result{RequeueAfter: 2 * time.Second}},
		"waiting for NodeClaims":   {expected: &reconcile.Result{RequeueAfter: 2 * time.Minute}},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			c := &WorkspaceReconciler{nodeProvisioner: &nodesReadyProvisioner{ready: tc.ready, needRequeue: tc.needRequeue}}
			result, err := c.reconcileNodes(context.Background(), &v1beta1.Workspace{})
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
