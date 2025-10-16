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

valid_languages = ['bash', 'c', 'c_sharp','commonlisp', 'cpp', 'css', 'dockerfile', 'dot', 'elisp', 'elixir', 'elm', 'embedded_template', 'erlang', 'fixed_form_fortran', 'fortran', 'go', 'gomod', 'hack', 'haskell', 'hcl', 'html', 'java', 'javascript', 'jsdoc', 'json', 'julia', 'kotlin', 'lua', 'make', 'markdown', 'objc', 'ocaml', 'perl', 'php', 'python', 'ql', 'r', 'regex', 'rst', 'ruby', 'rust', 'scala', 'sql', 'sqlite', 'toml', 'tsq', 'typescript', 'yaml']
file_extension_to_language_map = { ".sh": "bash", ".bash": "bash", ".c": "c", ".cs": "c_sharp", ".lisp": "commonlisp", ".lsp": "commonlisp", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".h": "c", ".css": "css", ".dockerfile": "dockerfile", "Dockerfile": "dockerfile", ".dot": "dot", ".el": "elisp", ".ex": "elixir", ".exs": "elixir", ".elm": "elm", ".ejs": "embedded_template", ".erl": "erlang", ".hrl": "erlang", ".f": "fixed_form_fortran", ".for": "fixed_form_fortran", ".f90": "fortran", ".f95": "fortran", ".go": "go", ".mod": "gomod", "go.mod": "gomod", ".hack": "hack", ".hs": "haskell", ".hcl": "hcl", ".tf": "hcl", ".html": "html", ".htm": "html", ".java": "java", ".js": "javascript", ".jsx": "javascript", ".jsdoc": "jsdoc", ".json": "json", ".jl": "julia", ".kt": "kotlin", ".kts": "kotlin", ".lua": "lua", ".mk": "make", "Makefile": "make", ".md": "markdown", ".m": "objc", ".mm": "objc", ".ml": "ocaml", ".mli": "ocaml", ".pl": "perl", ".pm": "perl", ".php": "php", ".py": "python", ".ql": "ql", ".r": "r", ".regex": "regex", ".rst": "rst", ".rb": "ruby", ".rs": "rust", ".scala": "scala", ".sc": "scala", ".sql": "sql", ".sqlite": "sqlite", ".db": "sqlite", ".toml": "toml", ".tsq": "tsq", ".ts": "typescript", ".tsx": "typescript", ".yaml": "yaml", ".yml": "yaml"}


def get_file_extension_language(file_extension: str) -> str:
    if not file_extension.startswith("."):
        file_extension = "." + file_extension
    return file_extension_to_language_map.get(file_extension, "")

def create_rag_document(text: str, metadata: dict[str, str] | None = None) -> dict[str, str]:
    document = {"text": text}
    if metadata:
        document["metadata"] = metadata
    return document