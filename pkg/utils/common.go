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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	errInvalidModelVersionURL = "invalid model version URL: %s. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>"
)

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
