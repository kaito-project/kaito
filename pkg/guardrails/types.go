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
)

// ScanMode defines how a scanner operates on input.
type ScanMode string

const (
	// ScanModeStreaming processes input incrementally with state.
	// Useful for handling text split across boundaries.
	ScanModeStreaming ScanMode = "streaming"

	// ScanModeBuffered processes complete input at once.
	// Simpler but requires buffering entire response.
	ScanModeBuffered ScanMode = "buffered"
)

// Action defines how to respond when a scanner detects a match.
type Action string

const (
	// ActionBlock stops processing and returns an error.
	ActionBlock Action = "block"

	// ActionRedact replaces matched content with a placeholder.
	ActionRedact Action = "redact"

	// ActionAllow allows the content to pass through (logging only).
	ActionAllow Action = "allow"
)

// Match represents a single detected violation.
type Match struct {
	// Start is the byte offset where the match begins.
	Start int

	// End is the byte offset where the match ends.
	End int

	// Content is the matched text.
	Content string

	// Severity indicates the severity level (e.g., "high", "medium", "low").
	Severity string
}

// Result is the output of a scan operation.
type Result struct {
	// Found indicates whether any matches were detected.
	Found bool

	// Matches is a list of detected violations.
	Matches []Match

	// Action specifies how to handle the detection.
	Action Action

	// Message provides optional context (e.g., reason for blocking).
	Message string
}

// Scanner defines the interface for a guardrail scanner.
type Scanner interface {
	// Name returns the scanner's identifier.
	Name() string

	// Mode returns how this scanner operates (streaming or buffered).
	Mode() ScanMode

	// Scan analyzes the input text and returns the result.
	// Streaming scanners may accumulate state across calls.
	Scan(ctx context.Context, text string) (Result, error)

	// Reset clears any accumulated state (for streaming mode).
	// Called when a scan session ends.
	Reset()
}

// ScannerConfig holds configuration for creating a scanner.
type ScannerConfig struct {
	// Type identifies the scanner (e.g., "regex", "secrets", "pii").
	Type string `json:"type"`

	// Action defines how to respond to detections.
	Action Action `json:"action"`

	// Enabled controls whether this scanner is active.
	Enabled bool `json:"enabled"`

	// Params contains scanner-specific configuration (e.g., patterns, thresholds).
	Params map[string]interface{} `json:"params,omitempty"`
}

// Config holds the complete guardrails configuration.
type Config struct {
	// Enabled turns guardrails on/off globally.
	Enabled bool `json:"enabled"`

	// BlockMessage is returned when a scanner blocks content.
	BlockMessage string `json:"blockMessage,omitempty"`

	// Scanners is the list of active scanners.
	Scanners []ScannerConfig `json:"scanners,omitempty"`
}
