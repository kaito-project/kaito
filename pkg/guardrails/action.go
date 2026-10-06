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
	"fmt"
)

// ActionHandler processes scan results based on the specified action.
type ActionHandler interface {
	Handle(text string, result Result) (string, error)
}

// BlockHandler returns an error when content is blocked.
type BlockHandler struct{}

func (bh *BlockHandler) Handle(text string, result Result) (string, error) {
	if !result.Found {
		return text, nil
	}
	msg := result.Message
	if msg == "" {
		msg = "Content blocked by guardrails"
	}
	return "", fmt.Errorf("%s", msg)
}

// RedactHandler replaces matched content with a placeholder.
type RedactHandler struct {
	Placeholder string
}

func (rh *RedactHandler) Handle(text string, result Result) (string, error) {
	if !result.Found || len(result.Matches) == 0 {
		return text, nil
	}

	placeholder := rh.Placeholder
	if placeholder == "" {
		placeholder = "[REDACTED]"
	}

	// Sort matches by start position (descending) to process from end to start.
	matches := make([]Match, len(result.Matches))
	copy(matches, result.Matches)
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Start < matches[j].Start {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	// Process matches from end to start to preserve offsets.
	output := text
	for _, match := range matches {
		if match.Start >= 0 && match.End > match.Start && match.End <= len(output) {
			output = output[:match.Start] + placeholder + output[match.End:]
		}
	}

	return output, nil
}

// AllowHandler allows content to pass through (no modification).
type AllowHandler struct{}

func (ah *AllowHandler) Handle(text string, result Result) (string, error) {
	return text, nil
}

