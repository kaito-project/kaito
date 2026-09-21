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

// Package progress reads live download-progress metrics from the sampler
// sidecar running in the model-mirror download Job pod.
//
// All progress arithmetic happens in the sampler (pkg/modelmirror/download/sampler.py).
// This package only fetches and parses, so the two cannot drift apart.
package progress

import (
	"context"
	"fmt"
	"io"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"k8s.io/client-go/kubernetes"
)

const (
	speedMetric     = "model_mirror_download_speed_bytes_per_second"
	remainingMetric = "model_mirror_download_remaining_seconds"

	metricsPort = "9100"
	metricsPath = "/metrics"

	// maxMetricsBytes bounds a misbehaving endpoint.
	maxMetricsBytes = 1 << 20
)

// Progress is the sampler's view of an in-flight download.
type Progress struct {
	// SpeedBytesPerSecond is the cumulative average since the download started.
	SpeedBytesPerSecond int64
	// RemainingSeconds is the estimated time to completion; -1 when unknown.
	RemainingSeconds int64
}

// Parse reads Prometheus text format and extracts the two gauges by name.
func Parse(r io.Reader) (*Progress, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("parsing sampler metrics: %w", err)
	}

	value := func(name string) (float64, error) {
		fam, ok := families[name]
		if !ok || len(fam.GetMetric()) == 0 {
			return 0, fmt.Errorf("sampler metrics missing %s", name)
		}
		return fam.GetMetric()[0].GetGauge().GetValue(), nil
	}

	speed, err := value(speedMetric)
	if err != nil {
		return nil, err
	}
	remaining, err := value(remainingMetric)
	if err != nil {
		return nil, err
	}

	return &Progress{
		SpeedBytesPerSecond: int64(speed),
		RemainingSeconds:    int64(remaining),
	}, nil
}

func ParseLimited(r io.Reader) (*Progress, error) {
	return Parse(io.LimitReader(r, maxMetricsBytes))
}

// Fetch GETs the sampler's /metrics through the API server's pod-proxy
// subresource.
func Fetch(ctx context.Context, cs kubernetes.Interface, namespace, podName string) (*Progress, error) {
	body, err := cs.CoreV1().
		Pods(namespace).
		ProxyGet("http", podName, metricsPort, metricsPath, nil).
		Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching sampler metrics from %s/%s: %w", namespace, podName, err)
	}
	defer body.Close()
	return ParseLimited(body)
}
