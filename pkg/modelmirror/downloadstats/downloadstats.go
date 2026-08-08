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

// Package downloadstats defines the contract between the model mirror download
// Job and the mirror controller. The Job's shell script emits a single marker
// line on success; the controller reads the pod log and parses it.
package downloadstats

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

const MarkerPrefix = "KAITO_DOWNLOAD_STATS"
const maxScanTokenSize = 1 << 20 // 1 MiB

// scanLinesOrCR is a bufio.SplitFunc that ends a token at a newline OR a bare
// carriage return.
// tqdm (used by the HuggingFace downloader) redraws in place by
// writing \r and overwriting the line.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Return the bytes before the first terminator, and consume the terminator
	// itself by advancing one byte past it.
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	return bufio.ScanLines(data, atEOF)
}

type Stats struct {
	// DurationSeconds is the wall-clock duration of the download.
	DurationSeconds int64
	// Bytes is the total size on disk of the downloaded model.
	Bytes int64
}

// ThroughputMBps returns the average throughput to two decimal places, or ""
// when the duration is zero.
func (s Stats) ThroughputMBps() string {
	if s.DurationSeconds <= 0 {
		return ""
	}
	mbps := float64(s.Bytes) / 1e6 / float64(s.DurationSeconds)
	return strconv.FormatFloat(mbps, 'f', 2, 64)
}

// Parse scans the reader for marker lines and returns the values from the last
// one. It returns (nil, nil) when no marker is present.
//
// An error is returned only when a marker is present but malformed, which
// indicates a real producer/consumer mismatch worth surfacing.
//
// The caller is responsible for closing r, and should bound it
func Parse(r io.Reader) (*Stats, error) {
	var lastMarker string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxScanTokenSize)
	scanner.Split(scanLinesOrCR)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, MarkerPrefix); idx != -1 {
			lastMarker = strings.TrimSpace(line[idx+len(MarkerPrefix):])
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) && lastMarker == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning download job logs: %w", err)
	}

	if lastMarker == "" {
		return nil, nil
	}

	stats := &Stats{}
	var sawSeconds, sawBytes bool
	// Iterate all fields; the last occurrence of each key wins
	for _, field := range strings.Fields(lastMarker) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing %s field %q: %w", MarkerPrefix, field, err)
		}
		switch key {
		case "seconds":
			stats.DurationSeconds, sawSeconds = parsed, true
		case "bytes":
			stats.Bytes, sawBytes = parsed, true
		}
	}

	if !sawSeconds || !sawBytes {
		return nil, fmt.Errorf("incomplete %s line %q: want seconds= and bytes=", MarkerPrefix, lastMarker)
	}

	return stats, nil
}

const maxLogReadBytes = 8 << 20 // 8 MiB
const logTailLines = int64(100)

// Fetch reads the download pod's logs and parses the stats marker. It returns
// (nil, nil) when the pod produced no marker.
func Fetch(ctx context.Context, cs kubernetes.Interface, namespace, podName string) (*Stats, error) {
	tail := logTailLines
	req := cs.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tail,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("streaming logs for pod %s/%s: %w", namespace, podName, err)
	}
	defer stream.Close()

	return Parse(io.LimitReader(stream, maxLogReadBytes))
}
