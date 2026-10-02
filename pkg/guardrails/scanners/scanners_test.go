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

package scanners

import (
	"context"
	"testing"

	"github.com/kaito-project/kaito/pkg/guardrails"
)

func TestRegexScanner(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "regex",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{
			"patterns": []interface{}{
				`secret\d+`,
				`password\s*=\s*\S+`,
			},
		},
	}

	scanner, err := NewRegexScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	if scanner.Name() != "regex" {
		t.Errorf("expected name 'regex', got %s", scanner.Name())
	}

	if scanner.Mode() != guardrails.ScanModeBuffered {
		t.Errorf("expected buffered mode")
	}

	text := "This contains secret123 and password = hidden"
	result, err := scanner.Scan(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !result.Found {
		t.Errorf("expected to find matches")
	}

	if len(result.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(result.Matches))
	}

	if result.Matches[0].Content != "secret123" {
		t.Errorf("expected 'secret123', got %q", result.Matches[0].Content)
	}
}

func TestRegexScannerNoMatch(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "regex",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{
			"patterns": []interface{}{
				`credit_card_\d+`,
			},
		},
	}

	scanner, err := NewRegexScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	result, err := scanner.Scan(context.Background(), "normal text")
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if result.Found {
		t.Errorf("expected no matches")
	}
}

func TestSecretsScanner(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "secrets",
		Action: guardrails.ActionBlock,
		Params: map[string]interface{}{
			"redactMode": "partial",
		},
	}

	scanner, err := NewSecretsScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	if scanner.Name() != "secrets" {
		t.Errorf("expected name 'secrets', got %s", scanner.Name())
	}

	// Test OpenAI API key detection: sk- followed by 20+ alphanumeric chars
	text := "API key: sk-1234567890abcdefghij in response"
	result, err := scanner.Scan(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !result.Found {
		t.Errorf("expected to find API key")
	}

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(result.Matches))
	}

	if result.Matches[0].Severity != "critical" {
		t.Errorf("expected critical severity for secret")
	}
}

func TestSecretsScannerAWSKey(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "secrets",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{},
	}

	scanner, err := NewSecretsScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	// AWS access key format: AKIA + 16 characters
	text := "AWS credentials: AKIAIOSFODNN7EXAMPLE in logs"
	result, err := scanner.Scan(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !result.Found {
		t.Errorf("expected to find AWS key")
	}
}

func TestSecretsScannerGithubToken(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "secrets",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{},
	}

	scanner, err := NewSecretsScanner(cfg)
	if err != nil {
		t.Fatalf("failed to create scanner: %v", err)
	}

	// GitHub Personal Access Token: ghp_ + 20+ chars
	text := "Token: ghp_1234567890123456789012345 in config"
	result, err := scanner.Scan(context.Background(), text)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if !result.Found {
		t.Errorf("expected to find GitHub token")
	}
}

func TestRegexScannerInvalidConfig(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "regex",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{}, // Missing patterns
	}

	_, err := NewRegexScanner(cfg)
	if err == nil {
		t.Errorf("expected error for missing patterns")
	}
}

func TestRegexScannerInvalidPattern(t *testing.T) {
	cfg := guardrails.ScannerConfig{
		Type:   "regex",
		Action: guardrails.ActionRedact,
		Params: map[string]interface{}{
			"patterns": []interface{}{
				`[invalid(regex`, // Invalid regex syntax
			},
		},
	}

	_, err := NewRegexScanner(cfg)
	if err == nil {
		t.Errorf("expected error for invalid regex")
	}
}
