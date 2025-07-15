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
	"log"
	"text/template"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed puller.sh
	pullerSHTextData string

	pullerSHTemplate *template.Template
)

func init() {
	t, err := template.New("puller.sh").Option("missingkey=zero").Parse(pullerSHTextData)
	if err != nil {
		panic(err)
	}

	pullerSHTemplate = t
}

func renderPullerSH(imgRef string, volDir string) string {
	normalizedImgRef, err := reference.ParseDockerRef(imgRef)
	if err != nil {
		log.Printf("failed to normalize image reference `%s`: %v", imgRef, err)
	}
	if normalizedImgRef != nil {
		imgRef = normalizedImgRef.String()
	}

	data := map[string]string{
		"imgRef": imgRef,
		"volDir": volDir,
	}

	var buf bytes.Buffer
	if err := pullerSHTemplate.Execute(&buf, data); err != nil {
		panic(err)
	}

	return buf.String()
}

func NewPullerContainer(inputImage string, outputDirectory string) *corev1.Container {
	return &corev1.Container{
		Name:  "puller",
		Image: "quay.io/skopeo/stable:v1.18.0-immutable",
		Command: []string{
			"/bin/sh",
			"-c",
		},
		Args: []string{
			renderPullerSH(inputImage, outputDirectory),
		},
	}
}
