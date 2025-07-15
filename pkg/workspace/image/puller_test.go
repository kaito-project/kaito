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
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Puller", func() {
	Context("embed", func() {
		It("initializes the puller text data", func() {
			Expect(pullerSHTextData).NotTo(BeEmpty())
		})

		It("uses sh as the interpreter", func() {
			Expect(pullerSHTextData).To(HavePrefix("#!/bin/sh\n"))
		})
	})

	Context("init", func() {
		It("instantiates the puller template", func() {
			Expect(pusherSHTemplate).NotTo(BeNil())
		})
	})

	Context("renderPullerSH", func() {
		It("renders the puller script", func() {
			var (
				imgRef = "docker.io/library/scratch:" + rand.String(8)
				volDir = "/tmp/" + rand.String(8)
			)

			ret := renderPullerSH(imgRef, volDir)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='` + imgRef + `'` + "\n"))
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${VOL_DIR}" ] || VOL_DIR='` + volDir + `'` + "\n"))
		})

		It("normalizes the image reference", func() {
			var (
				imgRef = "scratch-" + rand.String(8)
				volDir = "/tmp/" + rand.String(8)
			)

			ret := renderPullerSH(imgRef, volDir)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='docker.io/library/` + imgRef + `:latest'` + "\n"))
		})

		It("falls back to original image reference when unable to normalize", func() {
			var (
				imgRef = "!" + rand.String(8)
				volDir = "/tmp/" + rand.String(8)
			)

			ret := renderPullerSH(imgRef, volDir)
			Expect(ret).To(ContainSubstring("\n" + `[ ! -z "${IMG_REF}" ] || IMG_REF='` + imgRef + `'` + "\n"))
		})

		It("panics when the template is bad", func() {
			var (
				imgRef = "docker.io/library/scratch:" + rand.String(8)
				volDir = "/tmp/" + rand.String(8)
			)

			pullerSHTemplate0 := pullerSHTemplate
			defer func() {
				pullerSHTemplate = pullerSHTemplate0
			}()

			pullerSHTemplate = &template.Template{}

			f := func() {
				ret := renderPullerSH(imgRef, volDir)
				Expect(ret).To(BeEmpty())
			}
			Expect(f).To(PanicWith(MatchError(`template: : "" is an incomplete or empty template`)))
		})
	})

	Context("NewPullerContainer", func() {
		It("returns the expected container", func() {
			var (
				imgRef = "docker.io/library/scratch:" + rand.String(8)
				volDir = "/tmp/" + rand.String(8)
			)

			pullerSH := renderPullerSH(imgRef, volDir)

			ret := NewPullerContainer(imgRef, volDir)
			Expect(ret).NotTo(BeNil())
			Expect(ret.Name).To(Equal("puller"))
			Expect(ret.Image).To(Equal("quay.io/skopeo/stable:v1.18.0-immutable"))
			Expect(ret.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(ret.Args).To(Equal([]string{pullerSH}))
		})
	})
})
