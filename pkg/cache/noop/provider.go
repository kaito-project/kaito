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

// Package noop provides a no-op cache provider for testing and disabled mode.
package noop

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

const ProviderName = "noop"

// Provider is a no-op cache provider that always reports available and ready
// but performs no actual caching. Used for testing and as a fallback.
type Provider struct{}

var _ cache.Provider = (*Provider)(nil)
var _ cache.EventTarget = (*Provider)(nil)

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (p *Provider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "noop provider is always ready", nil
}

func (p *Provider) PodMutations(_ context.Context, _ cache.CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*cache.PodMutations, error) {
	return &cache.PodMutations{}, nil
}

func (p *Provider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}

func (p *Provider) EventObject() runtime.Object {
	return nil
}

// NewProvider returns a new noop cache provider instance.
func NewProvider() *Provider {
	return &Provider{}
}
