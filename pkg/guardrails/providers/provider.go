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

package providers

import (
	"fmt"
	"sync"

	"github.com/kaito-project/kaito/pkg/guardrails"
)

// Provider creates Scanner instances from configuration.
// Providers encapsulate complex scanner logic (e.g., ML models, external services).
type Provider interface {
	// Name returns the provider's identifier.
	Name() string

	// CreateScanner builds a Scanner from the given configuration.
	// Returns an error if the configuration is invalid.
	CreateScanner(cfg guardrails.ScannerConfig) (guardrails.Scanner, error)
}

// Registry manages available Scanner providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry.
// Returns an error if a provider with the same name already exists.
func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider must not be nil")
	}

	name := provider.Name()
	if name == "" {
		return fmt.Errorf("provider name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}

	r.providers[name] = provider
	return nil
}

// Get retrieves a provider by name.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}

	return provider, nil
}

// CreateScanner creates a Scanner using the provider matching the config type.
func (r *Registry) CreateScanner(cfg guardrails.ScannerConfig) (guardrails.Scanner, error) {
	provider, err := r.Get(cfg.Type)
	if err != nil {
		return nil, err
	}

	return provider.CreateScanner(cfg)
}

// List returns all registered provider names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	return names
}
