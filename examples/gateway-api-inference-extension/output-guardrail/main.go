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

	v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
)

// OpenAI /v1/chat/completions response format
type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type processorServer struct {
	v3.UnimplementedExternalProcessorServer
}

// Process handles the bidirectional stream of ProcessingRequest/ProcessingResponse.
// For PR2: parses OpenAI JSON, extracts choices[*].message.content.
func (s *processorServer) Process(
	stream v3.ExternalProcessor_ProcessServer,
) error {
	log.Println("=== Process() called ===")
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive error: %w", err)
		}

		// Only process response_body messages (headers/trailers are SKIPped in EnvoyFilter config)
		respBody := req.GetResponseBody()
		if respBody == nil {
			continue
		}

		log.Printf("Response body received, size=%d", len(respBody.GetBody()))

		// Attempt to parse OpenAI JSON and modify content
		modified, err := processOpenAIResponse(respBody.GetBody())
		if err != nil {
			log.Printf("Warning: failed to process response: %v", err)
			// fail-open: return original body on parse error
			modified = respBody.GetBody()
		}

		// Build ProcessingResponse with BodyResponse
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

// processOpenAIResponse parses OpenAI chat completion response and extracts content.
// This is where PR3 (guardrails) will hook in later.
// Returns the (possibly modified) response body.
func processOpenAIResponse(body []byte) ([]byte, error) {
	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("json parse error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	// For PR2: just extract and log content
	// PR3 will add guardrails.scanResponse() call here
	content := resp.Choices[0].Message.Content
	log.Printf("Extracted content: %s", content)

	// Re-serialize (unchanged for now, PR3 will modify content)
	modified, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %w", err)
	}

	return modified, nil
}

func main() {
	// Listen on port 9000
	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Println("Gateway Output Processor listening on :9000")

	// Create gRPC server and register ext_proc service
	grpcServer := grpc.NewServer()
	v3.RegisterExternalProcessorServer(grpcServer, &processorServer{})

	// Start server (blocking)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
