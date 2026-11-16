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

package env

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromContextRoundTrip(t *testing.T) {
	e := &Env{CloudProvider: "azure", Features: Features{EnableMIG: true}}
	assert.Same(t, e, FromContext(WithEnv(context.Background(), e)))
}

func TestFromContextDefault(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"not injected": context.Background(),
		"nil injected": WithEnv(context.Background(), nil),
	} {
		t.Run(name, func(t *testing.T) {
			e := FromContext(ctx)
			assert.Nil(t, e.Client)
			assert.Equal(t, Features{}, e.Features)
			assert.Empty(t, e.NodeProvisioner)
			assert.Empty(t, e.CloudProvider)
			assert.Empty(t, e.ReleaseNamespace)
			assert.Nil(t, e.SKUHandler)
			assert.Nil(t, e.NodeClassNames())
		})
	}
}
