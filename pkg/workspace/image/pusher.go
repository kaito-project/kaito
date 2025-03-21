package image

import (
	"bytes"
	"text/template"

	_ "embed"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed pusher.sh
	pusherSHTextData string

	pusherSHTemplate *template.Template
)

func init() {
	t, err := template.New("pusher.sh").Parse(pusherSHTextData)
	if err != nil {
		panic(err)
	}

	pusherSHTemplate = t
}

func renderPusherSH(volDir string, imgRef string) (string, error) {
	normalizedImgRef, err := reference.ParseDockerRef(imgRef)
	if err != nil {
		return "", err
	}

	data := map[string]string{
		"volDir": volDir,
		"imgRef": normalizedImgRef.String(),
	}

	var buf bytes.Buffer
	if err := pusherSHTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func NewPusherContainer(inputDirectory string, outputImage string) (*corev1.Container, error) {
	pusherSH, err := renderPusherSH(inputDirectory, outputImage)
	if err != nil {
		return nil, err
	}

	pusherContainer := corev1.Container{
		Name:  "pusher",
		Image: "ghcr.io/oras-project/oras:v1.2.2",
		Command: []string{
			"/bin/sh",
			"-c",
		},
		Args: []string{
			pusherSH,
		},
	}
	return &pusherContainer, nil
}
