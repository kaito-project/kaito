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

package image

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"log"
	"path"
	"text/template"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed pusher.sh
	pusherSHTextData string

	pusherSHTemplate *template.Template
)

func init() {
	t, err := template.New("pusher.sh").Option("missingkey=zero").Parse(pusherSHTextData)
	if err != nil {
		panic(err)
	}

	pusherSHTemplate = t
}

func renderPusherSH(volDir string, imgRef string, annotationsData map[string]map[string]string, sentinelPath *string) string {
	normalizedImgRef, err := reference.ParseDockerRef(imgRef)
	if err != nil {
		log.Printf("failed to normalize image reference `%s`: %v", imgRef, err)
	}
	if normalizedImgRef != nil {
		imgRef = normalizedImgRef.String()
	}

	data := map[string]string{
		"volDir":          volDir,
		"imgRef":          imgRef,
		"annotationsData": "{}",
		"sentinelPath":    path.Join(volDir, "fine_tuning_completed.txt"),
	}

	if annotationsData != nil {
		annotationsData, err := json.Marshal(annotationsData)
		if err != nil {
			panic(err)
		}

		data["annotationsData"] = string(annotationsData)
	}

	if sentinelPath != nil {
		data["sentinelPath"] = *sentinelPath
	}

	var buf bytes.Buffer
	if err := pusherSHTemplate.Execute(&buf, data); err != nil {
		panic(err)
	}

	return buf.String()
}

func NewPusherContainer(inputDirectory string, outputImage string, annotationsData map[string]map[string]string, sentinelPath *string) *corev1.Container {
	return &corev1.Container{
		Name:  "pusher",
		Image: "ghcr.io/oras-project/oras:v1.2.2",
		Command: []string{
			"/bin/sh",
			"-c",
		},
		Args: []string{
			renderPusherSH(inputDirectory, outputImage, annotationsData, sentinelPath),
		},
	}
}
