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

package helm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestNvidiaDevicePluginMIGStrategy(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Fatalf("Helm is required for chart rendering tests: %v", err)
	}

	tests := []struct {
		name         string
		values       []string
		wantPlugin   string
		wantGFD      string
		wantGFDUnset bool
		wantError    bool
	}{
		{name: "default", wantPlugin: "mixed"},
		{name: "explicit mixed", values: []string{"nvidiaDevicePlugin.migStrategy=mixed"}, wantPlugin: "mixed"},
		{name: "explicit single", values: []string{"nvidiaDevicePlugin.migStrategy=single"}, wantPlugin: "single"},
		{name: "explicit none", values: []string{"nvidiaDevicePlugin.migStrategy=none"}, wantPlugin: "none"},
		{name: "invalid", values: []string{"nvidiaDevicePlugin.migStrategy=invalid"}, wantError: true},
		{name: "empty", values: []string{"nvidiaDevicePlugin.migStrategy="}, wantError: true},
		{name: "null", values: []string{"nvidiaDevicePlugin.migStrategy=null"}, wantError: true},
		{name: "wrong type", values: []string{"nvidiaDevicePlugin.migStrategy=false"}, wantError: true},
		{name: "disabled", values: []string{"nvidiaDevicePlugin.enabled=false"}},
		{name: "disabled invalid", values: []string{"nvidiaDevicePlugin.enabled=false", "nvidiaDevicePlugin.migStrategy=invalid"}},
		{name: "bundled GFD default", values: []string{"featureGates.disableNodeAutoProvisioning=true"}, wantPlugin: "mixed", wantGFD: "mixed"},
		{name: "bundled GFD single", values: []string{"featureGates.disableNodeAutoProvisioning=true", "nvidiaDevicePlugin.migStrategy=single", "gpu-feature-discovery.migStrategy=single"}, wantPlugin: "single", wantGFD: "single"},
		{name: "bundled GFD none", values: []string{"featureGates.disableNodeAutoProvisioning=true", "nvidiaDevicePlugin.migStrategy=none", "gpu-feature-discovery.migStrategy=none"}, wantPlugin: "none", wantGFD: "none"},
		{name: "external plugin and GFD", values: []string{"featureGates.disableNodeAutoProvisioning=true", "nvidiaDevicePlugin.enabled=false", "gpu-feature-discovery.gfd.enabled=false", "gpu-feature-discovery.nfd.enabled=false"}},
		{name: "GFD ConfigMap strategy", values: []string{"featureGates.disableNodeAutoProvisioning=true", "nvidiaDevicePlugin.enabled=false", "gpu-feature-discovery.migStrategy=null", "gpu-feature-discovery.config.name=external-gpu-config"}, wantGFDUnset: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			args := []string{"template", "workspace-test", filepath.Join("..", "..", "charts", "kaito", "workspace"), "--namespace", "kaito-test", "--skip-tests"}
			for _, value := range tt.values {
				args = append(args, "--set", value)
			}
			command := exec.CommandContext(ctx, helm, args...)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if tt.wantError {
				if err == nil || !bytes.Contains(stderr.Bytes(), []byte("nvidiaDevicePlugin.migStrategy must be one of: none, single, mixed")) {
					t.Fatalf("expected MIG strategy validation failure, got %v:\n%s", err, &stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("helm template failed: %v:\n%s", err, &stderr)
			}
			if stderr.Len() > 0 {
				t.Logf("helm template diagnostics:\n%s", &stderr)
			}

			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
			seenPlugin, seenGFD := false, false
			for {
				var document json.RawMessage
				if err := decoder.Decode(&document); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("decode rendered manifest: %v", err)
				}
				var metadata metav1.TypeMeta
				if err := json.Unmarshal(document, &metadata); err != nil {
					t.Fatalf("decode manifest kind: %v", err)
				}
				if metadata.Kind != "DaemonSet" {
					continue
				}
				var daemonset appsv1.DaemonSet
				if err := json.Unmarshal(document, &daemonset); err != nil {
					t.Fatalf("decode DaemonSet: %v", err)
				}
				for _, container := range daemonset.Spec.Template.Spec.Containers {
					var expected string
					switch container.Name {
					case "nvidia-device-plugin-ctr":
						seenPlugin = true
						expected = tt.wantPlugin
					case "gpu-feature-discovery-ctr":
						seenGFD = true
						expected = tt.wantGFD
					default:
						continue
					}
					var strategies []string
					for _, env := range container.Env {
						if env.Name == "MIG_STRATEGY" {
							strategies = append(strategies, env.Value)
						}
					}
					if tt.wantGFDUnset && container.Name == "gpu-feature-discovery-ctr" {
						if len(strategies) != 0 {
							t.Errorf("GFD ConfigMap strategy overridden by MIG_STRATEGY=%v", strategies)
						}
					} else if len(strategies) != 1 || strategies[0] != expected {
						t.Errorf("%s MIG_STRATEGY=%v, want %q", container.Name, strategies, expected)
					}
				}
			}
			if seenPlugin != (tt.wantPlugin != "") {
				t.Errorf("bundled device plugin present=%t, want %t", seenPlugin, tt.wantPlugin != "")
			}
			if seenGFD != (tt.wantGFD != "" || tt.wantGFDUnset) {
				t.Errorf("bundled GFD present=%t, want %t", seenGFD, tt.wantGFD != "" || tt.wantGFDUnset)
			}
		})
	}
}
