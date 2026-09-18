FROM golang:1.26 AS builder

WORKDIR /build

# Copy KAITO go.mod/go.sum (has all dependencies including go-control-plane)
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy ext_proc server code
COPY examples/gateway-api-inference-extension/output-guardrail/main.go .

# Build static binary (no external deps needed in container)
RUN CGO_ENABLED=0 GOOS=linux go build -o ext-proc main.go

FROM gcr.io/distroless/static-debian12

# Copy only the binary
COPY --from=builder /build/ext-proc /

# Run as non-root (distroless already enforces this)
ENTRYPOINT ["/ext-proc"]
