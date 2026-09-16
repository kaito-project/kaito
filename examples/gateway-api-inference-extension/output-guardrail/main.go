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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"

	v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
)

// GuardrailsConfig holds scanner configuration (from env or file)
type GuardrailsConfig struct {
	BannedSubstrings []string
	BlockMessage     string
	Enabled          bool
}

// GuardrailsEngine scans content and applies redaction/blocking
type GuardrailsEngine struct {
	config *GuardrailsConfig
}

// NewGuardrailsEngine creates engine from environment
func NewGuardrailsEngine() *GuardrailsEngine {
	enabled := os.Getenv("GUARDRAILS_ENABLED") == "true"
	bannedStr := os.Getenv("BANNED_SUBSTRINGS")
	blockMsg := os.Getenv("BLOCK_MESSAGE")
	if blockMsg == "" {
		blockMsg = "Response blocked by guardrails"
	}

	var banned []string
	if bannedStr != "" {
		banned = strings.Split(bannedStr, ",")
		for i := range banned {
			banned[i] = strings.TrimSpace(banned[i])
		}
	}

	log.Printf("Guardrails enabled=%v, banned=%d substrings", enabled, len(banned))
	return &GuardrailsEngine{
		config: &GuardrailsConfig{
			BannedSubstrings: banned,
			BlockMessage:     blockMsg,
			Enabled:          enabled,
		},
	}
}

// ScanContent checks for banned substrings and returns modified content
func (ge *GuardrailsEngine) ScanContent(content string) string {
	if !ge.config.Enabled || len(ge.config.BannedSubstrings) == 0 {
		return content
	}

	lower := strings.ToLower(content)
	for _, banned := range ge.config.BannedSubstrings {
		if strings.Contains(lower, strings.ToLower(banned)) {
			log.Printf("Detected banned substring: %s, blocking", banned)
			return ge.config.BlockMessage
		}
	}
	return content
}

type processorServer struct {
	v3.UnimplementedExternalProcessorServer
	guardrails *GuardrailsEngine
}

// Process handles the bidirectional stream of ProcessingRequest/ProcessingResponse.
// For PR3: parses OpenAI JSON and applies guardrails scanners.
func (s *processorServer) Process(
	stream v3.ExternalProcessor_ProcessServer,
) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive error: %w", err)
		}

		// Only process response_body messages
		respBody := req.GetResponseBody()
		if respBody == nil {
			continue
		}

		log.Printf("Response body received, size=%d", len(respBody.GetBody()))

		// Parse and apply guardrails
		modified, err := s.processOpenAIResponse(respBody.GetBody())
		if err != nil {
			log.Printf("Warning: failed to process response: %v", err)
			modified = respBody.GetBody()
		}

		// Build ProcessingResponse
		resp := &v3.ProcessingResponse{
			Response: &v3.ProcessingResponse_ResponseBody{
				ResponseBody: &v3.BodyResponse{
					Response: &v3.CommonResponse{
						BodyMutation: &v3.BodyMutation{
							Mutation: &v3.BodyMutation_Body{
								Body: modified,
							},
						},
					},
				},
			},
		}

		// Send response back to Envoy
		if err := stream.Send(resp); err != nil {
			return fmt.Errorf("send error: %w", err)
		}
	}
}

// processOpenAIResponse parses OpenAI response, applies guardrails, and returns modified body.
// Preserves all original JSON fields for compatibility.
func (s *processorServer) processOpenAIResponse(body []byte) ([]byte, error) {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("json parse error: %w", err)
	}

	// Extract and process all choices
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	for _, c := range choices {
		choice, ok := c.(map[string]any)
		if !ok {
			continue
		}

		message, ok := choice["message"].(map[string]any)
		if !ok {
			continue
		}

		content, ok := message["content"].(string)
		if !ok {
			continue
		}

		// Apply guardrails scan
		scanned := s.guardrails.ScanContent(content)
		message["content"] = scanned
		log.Printf("Scanned content: len %d → %d", len(content), len(scanned))
	}

	// Re-serialize with all fields preserved
	modified, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %w", err)
	}

	return modified, nil
}

func main() {
	// Initialize guardrails
	guardrails := NewGuardrailsEngine()

	// Listen on port 9000
	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Println("Gateway Output Processor with Guardrails listening on :9000")

	// Create and start gRPC server
	grpcServer := grpc.NewServer()
	v3.RegisterExternalProcessorServer(grpcServer, &processorServer{guardrails: guardrails})

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
