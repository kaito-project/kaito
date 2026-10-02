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
	"fmt"
	"sync"
)

// EngineConfig holds configuration for the guardrails engine.
type EngineConfig struct {
	Enabled      bool
	BlockMessage string
	Scanners     []ScannerConfig
}

// Engine orchestrates multiple scanners to protect LLM output.
type Engine struct {
	mu       sync.RWMutex
	config   EngineConfig
	scanners map[string]Scanner
	handlers map[string]ActionHandler
}

// NewEngine creates a new guardrails engine.
func NewEngine() *Engine {
	return &Engine{
		scanners: make(map[string]Scanner),
		handlers: make(map[string]ActionHandler),
	}
}

// Configure sets the engine configuration and creates scanners from it.
// Requires a ScannerFactory to instantiate scanners from config.
func (e *Engine) Configure(cfg EngineConfig, factory ScannerFactory) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !cfg.Enabled {
		e.config = cfg
		e.scanners = make(map[string]Scanner)
		e.handlers = make(map[string]ActionHandler)
		return nil
	}

	// Create scanners from config
	scanners := make(map[string]Scanner)
	handlers := make(map[string]ActionHandler)

	for _, scanCfg := range cfg.Scanners {
		if !scanCfg.Enabled {
			continue
		}

		scanner, err := factory.CreateScanner(scanCfg)
		if err != nil {
			return fmt.Errorf("failed to create scanner %s: %w", scanCfg.Type, err)
		}

		scanners[scanCfg.Type] = scanner
		handlers[scanCfg.Type] = NewActionHandler(scanCfg.Action)
	}

	e.config = cfg
	e.scanners = scanners
	e.handlers = handlers
	return nil
}

// ScanText scans input text through all configured scanners.
// Returns the modified text (after applying redactions) or an error if blocked.
func (e *Engine) ScanText(ctx context.Context, text string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.config.Enabled || len(e.scanners) == 0 {
		return text, nil
	}

	output := text

	// Process each scanner in order
	for scannerType, scanner := range e.scanners {
		result, err := scanner.Scan(ctx, output)
		if err != nil {
			return "", fmt.Errorf("scanner %s failed: %w", scannerType, err)
		}

		if !result.Found {
			continue
		}

		handler := e.handlers[scannerType]
		if handler == nil {
			handler = &AllowHandler{}
		}

		modifiedText, err := handler.Handle(output, result)
		if err != nil {
			// Blocking action
			msg := e.config.BlockMessage
			if msg == "" {
				msg = "Content blocked by guardrails"
			}
			return "", fmt.Errorf("%s (scanner: %s)", msg, scannerType)
		}

		output = modifiedText

		// Reset scanner state after each scan (for streaming mode)
		scanner.Reset()
	}

	return output, nil
}

// IsEnabled returns whether guardrails are enabled.
func (e *Engine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config.Enabled
}

// ScannerList returns the list of active scanner types.
func (e *Engine) ScannerList() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	list := make([]string, 0, len(e.scanners))
	for scannerType := range e.scanners {
		list = append(list, scannerType)
	}
	return list
}

// ScannerFactory creates Scanner instances from configuration.
type ScannerFactory interface {
	CreateScanner(cfg ScannerConfig) (Scanner, error)
}
