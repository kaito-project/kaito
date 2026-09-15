package main

import (
	"fmt"
	"io"
	"log"
	"net"

	"github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
)

type processorServer struct {
	v3.UnimplementedExternalProcessorServer
}

// Process handles the bidirectional stream of ProcessingRequest/ProcessingResponse.
// For PoC: intercepts response_body and appends " [GATEWAY_TEST]" to prove mutation works.
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

		// Only process response_body messages (headers/trailers are SKIPped in EnvoyFilter config)
		respBody := req.GetResponseBody()
		if respBody == nil {
			continue
		}

		// Mutate: append " [GATEWAY_TEST]" to response body
		body := respBody.GetBody()
		modified := append(append([]byte{}, body...), []byte(" [GATEWAY_TEST]")...)

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
