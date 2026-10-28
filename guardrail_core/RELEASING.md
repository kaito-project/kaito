# Releasing KAITO Guardrail Core

Only `guardrail_core/` is built, not RAGEngine or the repository as a whole.
`pyproject.toml` is the single source of package metadata and version. Wheels
exclude tests; source distributions include tests and the build configuration.
The package license is a copy of the repository's Apache 2.0 license.

## Publisher setup

Confirm package ownership and maintainer access before enabling publication.
An administrator must configure:

- A project or pending publisher on both TestPyPI and PyPI for `kaito-guardrail-core`.
- A GitHub Trusted Publisher on each index: owner `kaito-project`, repository
  `kaito`, workflow filename `release-guardrail-core.yaml`, and the corresponding
  GitHub environment below. Publishing uses GitHub OIDC, not an API token.
- GitHub environments `guardrail-core-testpypi` and `guardrail-core-pypi`, restricted
  to `main`. Require maintainer approval for `guardrail-core-pypi` before enabling
  production publishing.

Reference: [kaito-rag-api release workflow](https://github.com/kaito-project/kaito-rag-api/blob/main/.github/workflows/create-release.yaml).
This package is published from KAITO; no changes to that repository are needed.

## Release workflow

After the intended package version merges into `main`, run the
[Release KAITO Guardrail Core workflow](../.github/workflows/release-guardrail-core.yaml):

1. Select the workflow on `main`. Supply `release_version`, such as `0.1.1`, and
   `release_commit`, the full lowercase 40-character commit SHA on `main`.
   The workflow checks ancestry, metadata/version agreement, and that the package
   tag does not already exist. It does not update the source version.
2. Build the source distribution and then its wheel. Validate metadata and
   distribution contents, then run all scanner and streaming tests against the
   installed wheel in a fresh environment outside the source checkout.
3. Publish those artifacts to TestPyPI. Download its wheel using only TestPyPI and
   compare it byte-for-byte with the original. Install dependencies separately
   from PyPI, then install the downloaded wheel in a fresh environment and smoke
   test the version, imports, framing, and scanners. No mixed-index resolution is
   used.
4. Approve the `guardrail-core-pypi` environment after verification succeeds.
   Publish the same stored wheel and source distribution to PyPI, without
   rebuilding.
5. Create `kaito-guardrail-core-v<version>` and a GitHub Release pointing to the exact
   source commit, with those distributions attached. Package tags do not trigger
   KAITO's application release workflow, and the package release is not marked as
   the repository's latest release.

## Recovery

For transient publishing or index availability failures, inspect the error and
rerun only failed jobs in the same workflow run to reuse the original artifacts.
Existing versions are not silently skipped or overwritten. If a partial upload
prevents retrying, coordinate recovery with the maintainers; do not rebuild
different content under the same version. After a successful release, bump the
version in a new PR before publishing again.

## Consumer adoption

Only after the version exists on production PyPI should RAGEngine and
production-stack adopt an exact dependency such as `kaito-guardrail-core==0.1.1`.
Production images should install requirements rather than copy the library source.
Local development may use editable installation. Track migration progress and
consumer changes in their PR descriptions, not in the package README.
