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

// RegexScanner detects patterns using compiled regular expressions.
type RegexScanner struct {
	name     string
	patterns []*regexp.Regexp
	action   guardrails.Action
}

// NewRegexScanner creates a RegexScanner from config.
func NewRegexScanner(cfg guardrails.ScannerConfig) (*RegexScanner, error) {
	if cfg.Type != "regex" {
		return nil, fmt.Errorf("invalid scanner type for RegexScanner: %s", cfg.Type)
	}

	patterns, ok := cfg.Params["patterns"].([]interface{})
	if !ok || len(patterns) == 0 {
		return nil, fmt.Errorf("RegexScanner requires 'patterns' parameter")
	}

	compiled := []*regexp.Regexp{}
	for i, p := range patterns {
		patternStr, ok := p.(string)
		if !ok {
			return nil, fmt.Errorf("pattern[%d] is not a string", i)
		}

		re, err := regexp.Compile(patternStr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile pattern[%d] %q: %w", i, patternStr, err)
		}

		compiled = append(compiled, re)
	}

	return &RegexScanner{
		name:     cfg.Type,
		patterns: compiled,
		action:   cfg.Action,
	}, nil
}

func (rs *RegexScanner) Name() string {
	return rs.name
}

func (rs *RegexScanner) Mode() guardrails.ScanMode {
	return guardrails.ScanModeBuffered
}

func (rs *RegexScanner) Scan(ctx context.Context, text string) (guardrails.Result, error) {
	result := guardrails.Result{
		Found:   false,
		Matches: []guardrails.Match{},
		Action:  rs.action,
	}

	for _, pattern := range rs.patterns {
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
					Severity: "high",
				})
			}
		}
	}

	return result, nil
}

func (rs *RegexScanner) Reset() {
	// No stateful data to reset
}
