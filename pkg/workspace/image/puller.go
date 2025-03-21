package image

import (
	"bytes"
	"text/template"

	_ "embed"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed puller.sh
	pullerSHTextData string

	pullerSHTemplate *template.Template
)

func init() {
	t, err := template.New("puller.sh").Parse(pullerSHTextData)
	if err != nil {
		panic(err)
	}

	pullerSHTemplate = t
}

func renderPullerSH(imgRef string, volDir string) (string, error) {
	normalizedImgRef, err := reference.ParseDockerRef(imgRef)
	if err != nil {
		return "", err
	}

	data := map[string]string{
		"imgRef": normalizedImgRef.String(),
		"volDir": volDir,
	}

	var buf bytes.Buffer
	if err := pullerSHTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func NewPullerContainer(inputImage string, outputDirectory string) (*corev1.Container, error) {
	pullerSH, err := renderPullerSH(inputImage, outputDirectory)
	if err != nil {
		return nil, err
	}

	pullerContainer := corev1.Container{
		Name:  "puller",
		Image: "quay.io/skopeo/stable:v1.18.0-immutable",
		Command: []string{
			"/bin/sh",
			"-c",
		},
		Args: []string{
			pullerSH,
		},
	}
	return &pullerContainer, nil
}
