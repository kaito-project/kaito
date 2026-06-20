# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Build-time patch: add fetch_images() to vLLM's MistralCommonImageProcessor.

vLLM 0.23.0 defines `MistralCommonImageProcessor` as a plain class that does NOT
subclass transformers' `ImageProcessingMixin`, so it has no `fetch_images`
method. transformers >=5.10 `ProcessorMixin.prepare_inputs_layout()` calls
`self.image_processor.fetch_images(images)` UNCONDITIONALLY during multimodal
startup profiling, so Mistral3/Pixtral vision models (e.g.
mistralai/Ministral-3-*-Instruct-2512, Mistral-Small-4-*) crash-loop with:

    AttributeError: 'MistralCommonImageProcessor' object has no attribute 'fetch_images'
    ValueError: Failed to apply MistralCommonPixtralProcessor

An upstream fix is available at vllm-project/vllm#45180 but not part of the 0.23.0 release.
This patch mirrors that fix by inserting a `fetch_images` method into the installed pixtral.py.

TODO: Remove this patch once the upstream issue is fixed.
"""

import py_compile
import sys

TARGET = "/usr/local/lib/python3.12/site-packages/vllm/transformers_utils/processors/pixtral.py"
ANCHOR = "        self.mm_encoder = mm_encoder"
GUARD_MARKER = "def fetch_images"
METHOD = """
    def fetch_images(self, image_url_or_urls):
        # KAITO patch: transformers >=5.10 prepare_inputs_layout() calls
        # image_processor.fetch_images() unconditionally, but vLLM's
        # MistralCommonImageProcessor does not subclass ImageProcessingMixin.
        # Mirror ImageProcessingMixin.fetch_images: pass through already-loaded
        # images and load URLs lazily.
        if isinstance(image_url_or_urls, (list, tuple)):
            return [self.fetch_images(x) for x in image_url_or_urls]
        if isinstance(image_url_or_urls, str):
            from transformers.image_utils import load_image

            return load_image(image_url_or_urls)
        return image_url_or_urls
"""


def main():
    with open(TARGET, encoding="utf-8") as f:
        src = f.read()

    if GUARD_MARKER in src:
        print(f"[patch] {TARGET} already patched (found '{GUARD_MARKER}'); skipping.")
        return

    count = src.count(ANCHOR)
    if count != 1:
        print(
            f"[patch] ERROR: expected exactly 1 occurrence of anchor "
            f"{ANCHOR!r} in {TARGET}, found {count}. vLLM layout changed; "
            f"update this patch.",
            file=sys.stderr,
        )
        sys.exit(1)

    src = src.replace(ANCHOR, ANCHOR + "\n" + METHOD, 1)
    with open(TARGET, "w", encoding="utf-8") as f:
        f.write(src)

    py_compile.compile(TARGET, doraise=True)
    print(f"[patch] added fetch_images() to MistralCommonImageProcessor in {TARGET}")


if __name__ == "__main__":
    main()
