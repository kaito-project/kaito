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
	"fmt"

	"github.com/kaito-project/kaito/pkg/guardrails"
)

// LLMGuardProvider wraps protectai/llm-guard scanners as a Provider.
// This isolates the llm-guard dependency to a single provider implementation.
// All llm-guard-specific logic is encapsulated here.
type LLMGuardProvider struct {
	name string
	// In a real implementation, this would hold llm-guard client/config
	// For this PoC, we show the structure without the actual llm-guard import
}

// NewLLMGuardProvider creates a new LLMGuard provider.
func NewLLMGuardProvider() *LLMGuardProvider {
	return &LLMGuardProvider{
		name: "llmguard",
	}
}

func (lp *LLMGuardProvider) Name() string {
	return lp.name
}

// CreateScanner creates a scanner for the given llm-guard scanner type.
// Supported types: "pii", "toxicity", "sensitive", "ban_topics", etc.
func (lp *LLMGuardProvider) CreateScanner(cfg guardrails.ScannerConfig) (guardrails.Scanner, error) {
	// Validate that we actually support this scanner type
	scannerType := cfg.Type
	if scannerType == "" {
		return nil, fmt.Errorf("LLMGuardProvider: scanner type not specified")
	}

	// Create a wrapper scanner that interfaces with llm-guard
	// In a real implementation, this would instantiate the appropriate
	// llm-guard scanner (Toxicity, PII, BanTopics, etc.)
	return &LLMGuardScanner{
		scannerType: scannerType,
		action:      cfg.Action,
		params:      cfg.Params,
	}, nil
}

// LLMGuardScanner wraps an llm-guard scanner instance.
type LLMGuardScanner struct {
	scannerType string
	action      guardrails.Action
	params      map[string]interface{}
	// In a real implementation, this would hold the actual llm-guard scanner
	// e.g., toxicity.ToxicityScanner, pii.PIIScanner, etc.
}

func (ls *LLMGuardScanner) Name() string {
	return "llmguard-" + ls.scannerType
}

func (ls *LLMGuardScanner) Mode() guardrails.ScanMode {
	// Most llm-guard scanners are buffered (require full text)
	// Some may support streaming with careful state management
	return guardrails.ScanModeBuffered
}

func (ls *LLMGuardScanner) Scan(ctx context.Context, text string) (guardrails.Result, error) {
	// This is a placeholder implementation.
	// In a real implementation, this would call the actual llm-guard scanner:
	//
	// Example (Toxicity):
	//   toxicity := toxicity.ToxicityScanner()
	//   result := toxicity.Scan(text)
	//   return guardrails.Result{
	//       Found: result.Found,
	//       Matches: convertToMatches(result),
	//       Action: ls.action,
	//   }, nil
	//
	// Example (PII):
	//   pii := pii.PIIScanner()
	//   result := pii.Scan(text, detectors=["email", "phone", "credit_card"])
	//   return guardrails.Result{...}, nil

	// For now, return "not found" to allow composition without hard llm-guard dependency
	return guardrails.Result{
		Found:  false,
		Action: ls.action,
	}, nil
}

func (ls *LLMGuardScanner) Reset() {
	// Clear any internal state from the wrapped llm-guard scanner
}
