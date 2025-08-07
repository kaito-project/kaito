# Release Management

## Overview

This document describes KAITO project release management, which includes release versioning, supported releases, and supported upgrades.

## Legend

- **X.Y.Z** refers to the version (git tag) of KAITO that is released. This is the version of the KAITO images and the Charts version.
- **Breaking changes** refer to schema changes, flag changes, and behavior changes of KAITO that may require a clean installation during upgrade, and it may introduce changes that could break backward compatibility.
- **Milestone** should be designed to include feature sets to accommodate 2 months release cycles including test gates. GitHub's milestones are used by maintainers to manage each release. PRs and Issues for each release should be created as part of a corresponding milestone.
- **Patch releases** refer to applicable fixes, including security fixes, may be backported to support releases, depending on severity and feasibility.
- **Test gates** should include soak tests and upgrade tests from the last minor version.

## Release Versioning

All releases will be of the form _vX.Y.Z_ where X is the major version, Y is the minor version and Z is the patch version. This project strictly follows semantic versioning.

The rest of the doc will cover the release process for the following kinds of releases:

**Major Releases**

No plan to move to 1.0.0 unless there is a major design change like an incompatible API change in the project

**Minor Releases**

- X.Y.0-rc.W, W >= 0 (Branch: release-X.Y)
    - Released as needed before we cut a stable X.Y release
    - Soak for a total of ~1 week before cutting a stable release
    - Release candidate release, cut from release-X.Y branch
- X.Y.0 (Branch: release-X.Y)
    - Released every ~2 months (with the exception of emergency minor releases to support new models)
    - Stable release, cut from release-X.Y branch when X.Y milestone is complete

**Patch Releases**

- Patch Releases X.Y.Z, Z > 0 (Branch: release-X.Y, only cut when a patch is needed)
    - No breaking changes
    - Applicable fixes, including security fixes, may be cherry-picked from main into the latest supported minor release-X.Y branches.
    - Patch release, cut from a release-X.Y branch

## Supported Releases

Applicable fixes, including security fixes, may be cherry-picked into the release branch, depending on severity and feasibility. Patch releases are cut from that branch as needed.

We expect users to stay reasonably up-to-date with the versions of KAITO they use in production, but understand that it may take time to upgrade. We expect users to be running approximately the latest patch release of a given minor release and encourage users to upgrade as soon as possible.

We expect to "support" n (current) and n-1 major.minor releases. "Support" means we expect users to be running that version in production. For example, when v0.5.0 comes out, v0.3.x will no longer be supported for patches, and we encourage users to upgrade to a supported version as soon as possible.

## Supported Kubernetes Versions

KAITO is assumed to be compatible with the [current Kubernetes Supported Versions](https://kubernetes.io/releases/patch-releases/#detailed-release-history-for-active-branches) per [Kubernetes Supported Versions policy](https://kubernetes.io/releases/version-skew-policy/).

For example, if KAITO _supported_ versions are v0.5 and v0.4, and Kubernetes _supported_ versions are v1.30, v1.31, v1.32, then all supported KAITO versions (v0.5, v0.4) are assumed to be compatible with all supported Kubernetes versions (v1.30, v1.31, v1.32). If Kubernetes v1.33 is released later, then KAITO v0.5 and v0.4 will be assumed to be compatible with v1.33 if those KAITO versions are still supported at that time.

If you choose to use KAITO with a version of Kubernetes that it does not support, you are using it at your own risk.

## Upgrades

Users should be able to run both X.Y and X.Y + 1 simultaneously in order to support gradual rollouts.

## Acknowledgement

This document builds on the ideas and implementations of release processes from projects like Kubernetes and Helm.
