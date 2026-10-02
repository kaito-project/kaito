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

package guardrails

import (
	"context"
	"testing"
)

// MockScannerFactory is a test factory for creating scanners.
type MockScannerFactory struct {
	scanners map[string]Scanner
	err      error
}

func (mf *MockScannerFactory) CreateScanner(cfg ScannerConfig) (Scanner, error) {
	if mf.err != nil {
		return nil, mf.err
	}
	return mf.scanners[cfg.Type], nil
}

// MockTestScanner detects a specific keyword and returns it as a match.
type MockTestScanner struct {
	keyword string
	action  Action
}

func (mts *MockTestScanner) Name() string   { return "mock-test" }
func (mts *MockTestScanner) Mode() ScanMode { return ScanModeBuffered }
func (mts *MockTestScanner) Scan(ctx context.Context, text string) (Result, error) {
	if mts.keyword != "" && contains(text, mts.keyword) {
		start := indexOf(text, mts.keyword)
		end := start + len(mts.keyword)
		return Result{
			Found: true,
			Matches: []Match{
				{Start: start, End: end, Content: mts.keyword, Severity: "high"},
			},
			Action: mts.action,
		}, nil
	}
	return Result{Found: false, Action: mts.action}, nil
}
func (mts *MockTestScanner) Reset() {}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestEngineDisabled(t *testing.T) {
	engine := NewEngine()

	cfg := EngineConfig{Enabled: false}
	factory := &MockScannerFactory{scanners: make(map[string]Scanner)}

	err := engine.Configure(cfg, factory)
	if err != nil {
		t.Fatalf("configure failed: %v", err)
	}

	if engine.IsEnabled() {
		t.Errorf("expected disabled engine")
	}

	// Even with content that would trigger, it should pass through
	text := "sensitive data here"
	result, err := engine.ScanText(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if result != text {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestEngineScanWithRedact(t *testing.T) {
	engine := NewEngine()

	scanner := &MockTestScanner{keyword: "secret", action: ActionRedact}
	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"secrets": scanner,
		},
	}

	cfg := EngineConfig{
		Enabled: true,
		Scanners: []ScannerConfig{
			{
				Type:    "secrets",
				Action:  ActionRedact,
				Enabled: true,
			},
		},
	}

	err := engine.Configure(cfg, factory)
	if err != nil {
		t.Fatalf("configure failed: %v", err)
	}

	text := "This is a secret message"
	result, err := engine.ScanText(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	expected := "This is a [REDACTED] message"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEngineScanWithBlock(t *testing.T) {
	engine := NewEngine()

	scanner := &MockTestScanner{keyword: "forbidden", action: ActionBlock}
	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"policy": scanner,
		},
	}

	cfg := EngineConfig{
		Enabled:      true,
		BlockMessage: "Content violates policy",
		Scanners: []ScannerConfig{
			{
				Type:    "policy",
				Action:  ActionBlock,
				Enabled: true,
			},
		},
	}

	err := engine.Configure(cfg, factory)
	if err != nil {
		t.Fatalf("configure failed: %v", err)
	}

	text := "This contains forbidden content"
	_, err = engine.ScanText(context.Background(), text)

	if err == nil {
		t.Errorf("expected error when blocked")
	}

	if err.Error() != "Content violates policy (scanner: policy)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEngineMultipleScanners(t *testing.T) {
	engine := NewEngine()

	scanner1 := &MockTestScanner{keyword: "credit_card", action: ActionRedact}
	scanner2 := &MockTestScanner{keyword: "ssn", action: ActionRedact}

	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"pii": scanner1,
			"ssn": scanner2,
		},
	}

	cfg := EngineConfig{
		Enabled: true,
		Scanners: []ScannerConfig{
			{Type: "pii", Action: ActionRedact, Enabled: true},
			{Type: "ssn", Action: ActionRedact, Enabled: true},
		},
	}

	err := engine.Configure(cfg, factory)
	if err != nil {
		t.Fatalf("configure failed: %v", err)
	}

	// Test both patterns
	text1 := "Card: credit_card here"
	result1, err := engine.ScanText(context.Background(), text1)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !contains(result1, "[REDACTED]") {
		t.Errorf("expected redaction for credit_card, got %q", result1)
	}

	// Second test for ssn
	text2 := "SSN: ssn here"
	result2, err := engine.ScanText(context.Background(), text2)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !contains(result2, "[REDACTED]") {
		t.Errorf("expected redaction for ssn, got %q", result2)
	}
}

func TestEngineScannerList(t *testing.T) {
	engine := NewEngine()

	scanner1 := &MockTestScanner{keyword: "test", action: ActionAllow}
	scanner2 := &MockTestScanner{keyword: "test", action: ActionAllow}

	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"scanner1": scanner1,
			"scanner2": scanner2,
		},
	}

	cfg := EngineConfig{
		Enabled: true,
		Scanners: []ScannerConfig{
			{Type: "scanner1", Action: ActionAllow, Enabled: true},
			{Type: "scanner2", Action: ActionAllow, Enabled: true},
		},
	}

	engine.Configure(cfg, factory)

	list := engine.ScannerList()
	if len(list) != 2 {
		t.Errorf("expected 2 scanners, got %d", len(list))
	}
}

func TestEngineConfigureWithDisabledScanner(t *testing.T) {
	engine := NewEngine()

	scanner := &MockTestScanner{keyword: "secret", action: ActionRedact}
	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"secrets": scanner,
		},
	}

	cfg := EngineConfig{
		Enabled: true,
		Scanners: []ScannerConfig{
			{
				Type:    "secrets",
				Action:  ActionRedact,
				Enabled: false, // Disabled
			},
		},
	}

	err := engine.Configure(cfg, factory)
	if err != nil {
		t.Fatalf("configure failed: %v", err)
	}

	// Disabled scanner should not be in the list
	if len(engine.ScannerList()) != 0 {
		t.Errorf("expected no enabled scanners, got %d", len(engine.ScannerList()))
	}
}

func TestEngineNoMatch(t *testing.T) {
	engine := NewEngine()

	scanner := &MockTestScanner{keyword: "nothere", action: ActionRedact}
	factory := &MockScannerFactory{
		scanners: map[string]Scanner{
			"test": scanner,
		},
	}

	cfg := EngineConfig{
		Enabled: true,
		Scanners: []ScannerConfig{
			{Type: "test", Action: ActionRedact, Enabled: true},
		},
	}

	engine.Configure(cfg, factory)

	text := "normal content here"
	result, err := engine.ScanText(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if result != text {
		t.Errorf("expected unchanged text, got %q", result)
	}
}
