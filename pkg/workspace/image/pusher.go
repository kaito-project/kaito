// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

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
