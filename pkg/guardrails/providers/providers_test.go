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
	"context"
	"testing"

	"github.com/kaito-project/kaito/pkg/guardrails"
)

// MockProvider is a test implementation of Provider.
type MockProvider struct {
	name string
}

func (mp *MockProvider) Name() string {
	return mp.name
}

func (mp *MockProvider) CreateScanner(cfg guardrails.ScannerConfig) (guardrails.Scanner, error) {
	return &MockScanner{name: mp.name}, nil
}

// MockScanner is a test implementation of Scanner.
type MockScanner struct {
	name string
}

func (ms *MockScanner) Name() string              { return ms.name }
func (ms *MockScanner) Mode() guardrails.ScanMode { return guardrails.ScanModeBuffered }
func (ms *MockScanner) Scan(ctx context.Context, text string) (guardrails.Result, error) {
	return guardrails.Result{}, nil
}
func (ms *MockScanner) Reset() {}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	// Initially empty
	if len(registry.List()) != 0 {
		t.Errorf("expected empty registry, got %d providers", len(registry.List()))
	}

	// Register a provider
	provider1 := &MockProvider{name: "mock1"}
	err := registry.Register(provider1)
	if err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	// List should now show 1
	if len(registry.List()) != 1 {
		t.Errorf("expected 1 provider, got %d", len(registry.List()))
	}

	// Get should work
	p, err := registry.Get("mock1")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}

	if p.Name() != "mock1" {
		t.Errorf("expected name 'mock1', got %s", p.Name())
	}
}

func TestRegistryDuplicateProvider(t *testing.T) {
	registry := NewRegistry()

	provider := &MockProvider{name: "duplicate"}
	_ = registry.Register(provider)

	// Try to register again
	err := registry.Register(provider)
	if err == nil {
		t.Errorf("expected error when registering duplicate provider")
	}
}

func TestRegistryProviderNotFound(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Get("nonexistent")
	if err == nil {
		t.Errorf("expected error for nonexistent provider")
	}
}

func TestRegistryCreateScanner(t *testing.T) {
	registry := NewRegistry()

	provider := &MockProvider{name: "test"}
	_ = registry.Register(provider)

	cfg := guardrails.ScannerConfig{
		Type:   "test",
		Action: guardrails.ActionRedact,
	}

	scanner, err := registry.CreateScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	if scanner == nil {
		t.Errorf("expected scanner, got nil")
	}
}

func TestLLMGuardProvider(t *testing.T) {
	provider := NewLLMGuardProvider()

	if provider.Name() != "llmguard" {
		t.Errorf("expected name 'llmguard', got %s", provider.Name())
	}

	cfg := guardrails.ScannerConfig{
		Type:   "toxicity",
		Action: guardrails.ActionBlock,
	}

	scanner, err := provider.CreateScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	if scanner == nil {
		t.Errorf("expected scanner, got nil")
	}

	if scanner.Name() != "llmguard-toxicity" {
		t.Errorf("expected name 'llmguard-toxicity', got %s", scanner.Name())
	}

	// Test scan (should return not found for placeholder)
	result, err := scanner.Scan(context.Background(), "test text")
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if result.Found {
		t.Errorf("expected not found for placeholder implementation")
	}
}

func TestLLMGuardProviderMultipleTypes(t *testing.T) {
	provider := NewLLMGuardProvider()

	scannerTypes := []string{"toxicity", "pii", "ban_topics", "sensitive"}

	for _, scannerType := range scannerTypes {
		cfg := guardrails.ScannerConfig{
			Type:   scannerType,
			Action: guardrails.ActionRedact,
		}

		scanner, err := provider.CreateScanner(cfg)
		if err != nil {
			t.Fatalf("failed to create %s scanner: %v", scannerType, err)
		}

		if scanner == nil {
			t.Errorf("expected scanner for type %s, got nil", scannerType)
		}

		expectedName := "llmguard-" + scannerType
		if scanner.Name() != expectedName {
			t.Errorf("expected name %q, got %q", expectedName, scanner.Name())
		}
	}
}

func TestRegistryMultipleProviders(t *testing.T) {
	registry := NewRegistry()

	p1 := &MockProvider{name: "provider1"}
	p2 := &MockProvider{name: "provider2"}
	p3 := &MockProvider{name: "provider3"}

	_ = registry.Register(p1)
	_ = registry.Register(p2)
	_ = registry.Register(p3)

	if len(registry.List()) != 3 {
		t.Errorf("expected 3 providers, got %d", len(registry.List()))
	}

	// All should be retrievable
	for _, name := range []string{"provider1", "provider2", "provider3"} {
		_, err := registry.Get(name)
		if err != nil {
			t.Errorf("failed to get %s: %v", name, err)
		}
	}
}
