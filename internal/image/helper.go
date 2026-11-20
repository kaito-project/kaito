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

const (
	// PythonHelperImage is the Python image for helper containers that pip-install their
	// dependencies at start. Run PipBootstrapCommand before any other pip install.
	PythonHelperImage = "mcr.microsoft.com/azurelinux/base/python:3.12"

	// PipBootstrapCommand replaces the image's pip with a hash-pinned release. The image's
	// pip is rpm-installed (no RECORD file), so --ignore-installed is required.
	PipBootstrapCommand = `pip install --no-cache-dir -q --ignore-installed "pip @ https://files.pythonhosted.org/packages/3f/06/3e4420302363fbb71be3c7f35ce042f54bfca330e3e66b3a2e3c4cca579f/pip-26.2.2-py3-none-any.whl#sha256=fd7f4dfa8190e014013b9f08f3e973f4e4c26ffd179a204895533a7254813a70"`
)
