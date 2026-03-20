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

package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const (
	// benchmarkResultTag is the log line tag emitted by benchmark_entrypoint.py.
	benchmarkResultTag = "KAITO_BENCHMARK_RESULT"

	// benchmarkPodIndexSuffix is appended to the StatefulSet name to get the leader pod name.
	// The benchmark always runs on POD_INDEX=0.
	benchmarkPodIndexSuffix = "-0"

	// benchmarkLogTailLines limits how many lines we read from the tail of the pod log.
	// The result line is always near the end of the startup sequence.
	benchmarkLogTailLines = int64(500)
)

// benchmarkResultPayload mirrors the JSON emitted by benchmark_entrypoint.py.
type benchmarkResultPayload struct {
	VLLMTotalTPM float64 `json:"vllm_total_tpm"`
	// TTFTAvgMs (time-to-first-token, ms) and TPOTAvgMs (time-per-output-token, ms) are parsed
	// from the benchmark output but not yet surfaced in BenchmarkResult. Reserved for future use
	TTFTAvgMs float64 `json:"ttft_avg_ms"`
	TPOTAvgMs float64 `json:"tpot_avg_ms"`
}

// parseBenchmarkResult scans pod log lines for the last KAITO_BENCHMARK_RESULT entry
// and returns the parsed metrics.
//
// Log line format (emitted by benchmark_entrypoint.py):
//
//	KAITO_BENCHMARK_RESULT <RFC3339-timestamp> <JSON-payload>
//
// Multiple result lines may be present if the startup probe failed and retried.
// We always take the last occurrence, which is guaranteed to be the successful one
// (exit 0 stops further probe ticks).
func parseBenchmarkResult(logs string) (*kaitov1beta1.BenchmarkResult, error) {
	var lastPayload string

	scanner := bufio.NewScanner(strings.NewReader(logs))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, benchmarkResultTag)
		if idx == -1 {
			continue
		}
		// Everything after the tag: "<timestamp> <json>"
		rest := strings.TrimSpace(line[idx+len(benchmarkResultTag):])
		// Skip the timestamp token (first word after tag).
		spaceIdx := strings.Index(rest, " ")
		if spaceIdx == -1 {
			continue
		}
		lastPayload = strings.TrimSpace(rest[spaceIdx+1:])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning pod logs: %w", err)
	}

	if lastPayload == "" {
		return nil, fmt.Errorf("no %s line found in pod logs", benchmarkResultTag)
	}

	var payload benchmarkResultPayload
	if err := json.Unmarshal([]byte(lastPayload), &payload); err != nil {
		return nil, fmt.Errorf("parsing benchmark result JSON %q: %w", lastPayload, err)
	}

	return &kaitov1beta1.BenchmarkResult{
		TokensPerMinute: strconv.FormatFloat(payload.VLLMTotalTPM, 'f', -1, 64),
	}, nil
}

type podLogStreamer interface {
	StreamLogs(ctx context.Context, namespace, podName string) (io.ReadCloser, error)
}

type kubeClientStreamer struct {
	kubeClient kubernetes.Interface
}

func (s *kubeClientStreamer) StreamLogs(ctx context.Context, namespace, podName string) (io.ReadCloser, error) {
	tailLines := benchmarkLogTailLines
	req := s.kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	})
	return req.Stream(ctx)
}

// reconcileBenchmarkResult reads the leader pod's logs (POD_INDEX=0) and parses
// the last KAITO_BENCHMARK_RESULT line. It is called only when the workspace
// inference is ready and the benchmark annotation is set.
func reconcileBenchmarkResult(ctx context.Context, wObj *kaitov1beta1.Workspace, streamer podLogStreamer) (*kaitov1beta1.BenchmarkResult, error) {
	podName := wObj.Name + benchmarkPodIndexSuffix

	stream, err := streamer.StreamLogs(ctx, wObj.Namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("streaming logs for pod %s/%s: %w", wObj.Namespace, podName, err)
	}
	defer stream.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading log stream for pod %s/%s: %w", wObj.Namespace, podName, err)
	}

	result, err := parseBenchmarkResult(sb.String())
	if err != nil {
		return nil, fmt.Errorf("pod %s/%s: %w", wObj.Namespace, podName, err)
	}

	klog.InfoS("benchmark result parsed", "workspace", klog.KObj(wObj),
		"tokensPerMinute", result.TokensPerMinute)

	return result, nil
}
