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

from setuptools import setup
from setuptools.command.build_py import build_py


class BuildPy(build_py):
    def find_package_modules(self, package, package_dir):
        # Scanner tests currently live alongside the implementation.
        return [
            entry
            for entry in super().find_package_modules(package, package_dir)
            if not entry[1].startswith("test_")
        ]


setup(cmdclass={"build_py": BuildPy})
