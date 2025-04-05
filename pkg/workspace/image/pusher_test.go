// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package image

import (
	"encoding/json"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Pusher", func() {
	Context("embed", func() {
		It("initializes the pusher text data", func() {
			Expect(pusherSHTextData).NotTo(BeEmpty())
		})

		It("uses sh as the interpreter", func() {
			Expect(pusherSHTextData).To(HavePrefix("#!/bin/sh\n"))
		})
	})

	Context("init", func() {
		It("instantiates the pusher template", func() {
			Expect(pusherSHTemplate).NotTo(BeNil())
		})
	})

	Context("renderPusherSH", func() {
		It("renders the pusher script", func() {
			var (
				volDir          = "/tmp/" + rand.String(8)
				imgRef          = "docker.io/library/scratch:" + rand.String(8)
				annotationsData = map[string]map[string]string{"$config": {"hello": "world"}, "$manifest": {"foo": "bar"}}
				sentinelPath    = "/tmp/" + rand.String(8)
			)

			bytes, err := json.Marshal(annotationsData)
			Expect(err).NotTo(HaveOccurred())

			ret := renderPusherSH(volDir, imgRef, annotationsData, &sentinelPath)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${VOL_DIR}" ] || VOL_DIR='` + volDir + `'` + "\n"))
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='` + imgRef + `'` + "\n"))
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z '' ] || ANNOTATIONS_DATA='` + string(bytes) + `'` + "\n"))
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z '' ] || SENTINEL_PATH='` + sentinelPath + `'` + "\n"))
		})

		It("normalizes the image reference", func() {
			var (
				volDir = "/tmp/" + rand.String(8)
				imgRef = "scratch-" + rand.String(8)
			)

			ret := renderPusherSH(volDir, imgRef, nil, nil)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='docker.io/library/` + imgRef + `:latest'` + "\n"))
		})

		It("falls back to original image reference when unable to normalize", func() {
			var (
				volDir = "/tmp/" + rand.String(8)
				imgRef = "^" + rand.String(8)
			)

			ret := renderPusherSH(volDir, imgRef, nil, nil)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='` + imgRef + `'` + "\n"))
		})

		It("panics when the template is bad", func() {
			var (
				volDir = "/tmp/" + rand.String(8)
				imgRef = "docker.io/library/scratch:" + rand.String(8)
			)

			pusherSHTemplate0 := pusherSHTemplate
			defer func() {
				pusherSHTemplate = pusherSHTemplate0
			}()

			pusherSHTemplate = &template.Template{}

			f := func() {
				ret := renderPusherSH(volDir, imgRef, nil, nil)
				Expect(ret).To(BeEmpty())
			}
			Expect(f).To(PanicWith(MatchError(`template: : "" is an incomplete or empty template`)))
		})
	})

	Context("NewPusherContainer", func() {
		It("returns the expected container", func() {
			var (
				volDir = "/tmp/" + rand.String(8)
				imgRef = "docker.io/library/scratch:" + rand.String(8)
			)

			pusherSH := renderPusherSH(volDir, imgRef, nil, nil)

			ret := NewPusherContainer(volDir, imgRef, nil, nil)
			Expect(ret).NotTo(BeNil())
			Expect(ret.Name).To(Equal("pusher"))
			Expect(ret.Image).To(Equal("ghcr.io/oras-project/oras:v1.2.2"))
			Expect(ret.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(ret.Args).To(Equal([]string{pusherSH}))
		})
	})
})
