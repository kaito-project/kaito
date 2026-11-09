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

package utils

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/mig"
)

const (
	errInvalidModelVersionURL = "invalid model version URL: %s. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>"
)

func Contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// GetMIGGPUConfig builds a GPUConfig from a MIG profile string. Each MIG workload
// is scheduled on exactly one hardware-isolated slice.
func GetMIGGPUConfig(migProfile string) (*sku.GPUConfig, error) {
	_, memGB, err := mig.ParseMIGProfile(migProfile)
	if err != nil {
		return nil, fmt.Errorf("invalid MIG profile: %w", err)
	}
	return &sku.GPUConfig{
		SKU:      "unknown",
		GPUCount: 1,
		GPUMem:   resource.MustParse(fmt.Sprintf("%dGi", memGB)),
		IsMIG:    true,
	}, nil
}

// SearchRawExtension performs a search for a key within a runtime.RawExtension.
func SearchRawExtension(raw runtime.RawExtension, key string) (interface{}, bool, error) {
	var data map[string]interface{}
	if err := yaml.Unmarshal(raw.Raw, &data); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal runtime.RawExtension: %w", err)
	}

	result, found := data[key]
	if !found {
		return nil, false, nil
	}

	return result, true, nil
}

func BuildCmdStr(baseCommand string, runParams ...map[string]string) string {
	updatedBaseCommand := baseCommand
	for _, runParam := range runParams {
		for key, value := range runParam {
			if value == "" {
				updatedBaseCommand = fmt.Sprintf("%s --%s", updatedBaseCommand, key)
			} else {
				updatedBaseCommand = fmt.Sprintf("%s --%s=%s", updatedBaseCommand, key, value)
			}
		}
	}

	return updatedBaseCommand
}

func BuildIfElseCmdStr(condition string, trueCmd string, trueCmdParams map[string]string, falseCmd string, falseCmdParams map[string]string) string {
	trueCmdStr := BuildCmdStr(trueCmd, trueCmdParams)
	falseCmdStr := BuildCmdStr(falseCmd, falseCmdParams)
	return fmt.Sprintf("if %s; then %s; else %s; fi", condition, trueCmdStr, falseCmdStr)
}

func ShellCmd(command string) []string {
	return []string{
		"/bin/sh",
		"-c",
		command,
	}
}

func GetReleaseNamespace() (string, error) {
	// Path to the namespace file inside a Kubernetes pod
	namespaceFilePath := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	// Attempt to read the namespace from the file
	if content, err := os.ReadFile(namespaceFilePath); err == nil {
		return string(content), nil
	}

	// Fallback: Read the namespace from an environment variable
	if namespace, exists := os.LookupEnv(consts.DefaultReleaseNamespaceEnvVar); exists {
		return namespace, nil
	}
	return "", fmt.Errorf("failed to determine release namespace from file %s and env var %s", namespaceFilePath, consts.DefaultReleaseNamespaceEnvVar)
}

func ExtractAndValidateRepoName(image string) error {
	// Extract repository name (part after the last / and before the colon :)
	// For example given image: modelsregistry.azurecr.io/ADAPTER_HERE:0.0.1
	parts := strings.Split(image, "/")
	lastPart := parts[len(parts)-1]             // Extracts "ADAPTER_HERE:0.0.1"
	repoName := strings.Split(lastPart, ":")[0] // Extracts "ADAPTER_HERE"

	// Check if repository name is lowercase
	if repoName != strings.ToLower(repoName) {
		return fmt.Errorf("repository name must be lowercase")
	}

	return nil
}

// ParseHuggingFaceModelVersion parses the model version in the format of https://huggingface.co/<org>/<model>/commit/<revision>
// and returns the repoId and revision. If the commit is not specified, it returns an empty string for revision,
// and the main branch HEAD commit is used.
//
// Example 1:
//
//	Version: "https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8"
//	RepoId: "tiiuae/falcon-7b"
//	Revision: "ec89142b67d748a1865ea4451372db8313ada0d8"
//
// Example 2:
//
//	Version: https://huggingface.co/tiiuae/falcon-7b
//	RepoId: "tiiuae/falcon-7b"
//	Revision: "" (main branch HEAD commit is used)
func ParseHuggingFaceModelVersion(version string) (repoId string, revision string, err error) {
	parsedURL, err := url.Parse(version)
	if err != nil {
		return "", "", err
	}

	if parsedURL.Host != "huggingface.co" {
		return "", "", fmt.Errorf(errInvalidModelVersionURL, version)
	}

	parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	switch len(parts) {
	case 2: // Expected path: "<org>/<model>"
		repoId, revision = parts[0]+"/"+parts[1], ""
		return
	case 4: // Expected path: "<org>/<model>/commit/<revision>"
		if parts[2] != "commit" {
			break
		}
		repoId, revision = parts[0]+"/"+parts[1], parts[3]
		return
	}

	return "", "", fmt.Errorf(errInvalidModelVersionURL, version)
}

// getRayLeaderHost constructs the leader host for the Ray cluster.
func GetRayLeaderHost(meta metav1.ObjectMeta) string {
	return fmt.Sprintf("%s-0.%s-headless.%s.svc.cluster.local",
		meta.Name, meta.Name, meta.Namespace)
}

// InferencePoolName returns the name of the inference pool for the given workspace.
func InferencePoolName(workspaceName string) string {
	return fmt.Sprintf("%s-inferencepool", workspaceName)
}
