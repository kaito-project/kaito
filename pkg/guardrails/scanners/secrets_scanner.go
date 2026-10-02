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
	"fmt"
	"regexp"

	"github.com/kaito-project/kaito/pkg/guardrails"
)

// SecretsScanner detects common secret patterns (API keys, tokens, etc).
type SecretsScanner struct {
	name      string
	patterns  []*regexp.Regexp
	action    guardrails.Action
	redactLen int // partial redact mode: keep first N chars visible
}

// NewSecretsScanner creates a SecretsScanner from config.
func NewSecretsScanner(cfg guardrails.ScannerConfig) (*SecretsScanner, error) {
	if cfg.Type != "secrets" {
		return nil, fmt.Errorf("invalid scanner type for SecretsScanner: %s", cfg.Type)
	}

	// Build default secret patterns if not provided
	patterns := defaultSecretPatterns

	// Check if redactMode is specified
	redactLen := 0
	if redactMode, ok := cfg.Params["redactMode"].(string); ok && redactMode == "partial" {
		redactLen = 4 // Keep first 4 chars visible: "sk-a...***"
	}

	compiled := []*regexp.Regexp{}
	for _, patternStr := range patterns {
		re, err := regexp.Compile(patternStr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile secret pattern %q: %w", patternStr, err)
		}
		compiled = append(compiled, re)
	}

	return &SecretsScanner{
		name:      cfg.Type,
		patterns:  compiled,
		action:    cfg.Action,
		redactLen: redactLen,
	}, nil
}

func (ss *SecretsScanner) Name() string {
	return ss.name
}

func (ss *SecretsScanner) Mode() guardrails.ScanMode {
	return guardrails.ScanModeBuffered
}

func (ss *SecretsScanner) Scan(ctx context.Context, text string) (guardrails.Result, error) {
	result := guardrails.Result{
		Found:   false,
		Matches: []guardrails.Match{},
		Action:  ss.action,
	}

	for _, pattern := range ss.patterns {
		matches := pattern.FindAllStringIndex(text, -1)
		if len(matches) > 0 {
			result.Found = true

			for _, match := range matches {
				start, end := match[0], match[1]
				content := text[start:end]

				result.Matches = append(result.Matches, guardrails.Match{
					Start:    start,
					End:      end,
					Content:  content,
					Severity: "critical",
				})
			}
		}
	}

	return result, nil
}

func (ss *SecretsScanner) Reset() {
	// No stateful data to reset
}

// defaultSecretPatterns are common API key and token patterns.
var defaultSecretPatterns = []string{
	// AWS Access Key
	`\bAKIA[0-9A-Z]{16}\b`,
	// Google API Key
	`\bAIza[0-9A-Za-z\-_]{35}\b`,
	// GitHub Token
	`\b(?:ghp|ghu|ghs|gho)_[A-Za-z0-9_]{20,}\b`,
	// OpenAI API Key
	`(?i)\bsk-[A-Za-z0-9]{20,}\b`,
	// Generic Bearer Token
	`(?i)\bbearer\s+[A-Za-z0-9._\-]{20,}\b`,
	// Private Key (PEM format)
	`-----BEGIN (?:[A-Z ]+)?PRIVATE KEY-----`,
}
