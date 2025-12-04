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

#!/usr/bin/env python3
"""
Entry point for RAGEngine lifecycle hooks.
Handles PostStart and PreStop events for FAISS index persistence.

Usage:
    python3 hooks.py poststart
    python3 hooks.py prestop
"""

import sys

from manager import poststart_handler, prestop_handler


def main():
    if len(sys.argv) != 2:
        print("Usage: hooks.py [poststart|prestop]")
        sys.exit(1)

    command = sys.argv[1].lower()

    if command == "poststart":
        exit_code = poststart_handler()
    elif command == "prestop":
        exit_code = prestop_handler()
    else:
        print(f"Unknown command: {command}")
        print("Usage: hooks.py [poststart|prestop]")
        sys.exit(1)

    sys.exit(exit_code)


if __name__ == "__main__":
    main()
