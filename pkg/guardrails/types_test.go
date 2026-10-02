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

// MockScanner is a test implementation of Scanner.
type MockScanner struct {
	name     string
	mode     ScanMode
	scanFunc func(ctx context.Context, text string) (Result, error)
}

func (ms *MockScanner) Name() string   { return ms.name }
func (ms *MockScanner) Mode() ScanMode { return ms.mode }
func (ms *MockScanner) Scan(ctx context.Context, text string) (Result, error) {
	return ms.scanFunc(ctx, text)
}
func (ms *MockScanner) Reset() {}

func TestScannerInterface(t *testing.T) {
	scanner := &MockScanner{
		name: "test-scanner",
		mode: ScanModeBuffered,
		scanFunc: func(ctx context.Context, text string) (Result, error) {
			return Result{Found: false}, nil
		},
	}

	if scanner.Name() != "test-scanner" {
		t.Errorf("expected name 'test-scanner', got %s", scanner.Name())
	}

	if scanner.Mode() != ScanModeBuffered {
		t.Errorf("expected mode ScanModeBuffered, got %s", scanner.Mode())
	}

	result, err := scanner.Scan(context.Background(), "test text")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result.Found {
		t.Errorf("expected Found=false, got true")
	}
}

func TestActionParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected Action
	}{
		{"block", ActionBlock},
		{"BLOCK", ActionBlock},
		{" block ", ActionBlock},
		{"redact", ActionRedact},
		{"REDACT", ActionRedact},
		{"allow", ActionAllow},
		{"ALLOW", ActionAllow},
		{"unknown", ActionAllow}, // defaults to allow
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseAction(tt.input)
			if got != tt.expected {
				t.Errorf("ParseAction(%q) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRedactHandler(t *testing.T) {
	handler := &RedactHandler{Placeholder: "[REDACTED]"}

	text := "Hello my secret123 world"
	// "secret123" starts at index 9 and ends at 18 (exclusive)
	result := Result{
		Found: true,
		Matches: []Match{
			{Start: 9, End: 18, Content: "secret123", Severity: "high"},
		},
		Action: ActionRedact,
	}

	output, err := handler.Handle(text, result)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expected := "Hello my [REDACTED] world"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestBlockHandler(t *testing.T) {
	handler := &BlockHandler{}

	result := Result{
		Found:   true,
		Action:  ActionBlock,
		Message: "Content blocked",
	}

	text := "blocked content"
	output, err := handler.Handle(text, result)

	if err == nil {
		t.Errorf("expected error, got nil")
	}

	if output != "" {
		t.Errorf("expected empty output on block, got %q", output)
	}
}

func TestAllowHandler(t *testing.T) {
	handler := &AllowHandler{}

	result := Result{
		Found:  false,
		Action: ActionAllow,
	}

	text := "allowed content"
	output, err := handler.Handle(text, result)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if output != text {
		t.Errorf("expected %q, got %q", text, output)
	}
}
