# Documentation Versioning Workflow

This document explains how the automated documentation versioning workflow works in the Kaito project.

## Overview

The `docs-versioning.yaml` GitHub Actions workflow automatically creates versioned documentation when new minor versions are released. This ensures that users can access documentation for specific versions of Kaito.

## How it Works

### Trigger Conditions

The workflow triggers when a new tag is pushed that matches the pattern `v[0-9]+.[0-9]+.0`, which means:
- ✅ `v0.6.0` - Will trigger (new minor version)
- ✅ `v0.7.0` - Will trigger (new minor version) 
- ✅ `v1.0.0` - Will trigger (new major version)
- ❌ `v0.5.1` - Will NOT trigger (patch version)
- ❌ `v0.5.2` - Will NOT trigger (patch version)

### Version Format Conversion

The workflow converts semantic version tags to documentation version format:
- `v0.6.0` → `0.6.x` (documentation version)
- `v0.7.0` → `0.7.x` (documentation version)
- `v1.0.0` → `1.0.x` (documentation version)

This follows the pattern where each minor version series (0.6.x, 0.7.x) gets its own documentation version.

### Workflow Steps

1. **Checkout**: Checks out the `main` branch to get the latest documentation content
2. **Setup**: Installs Node.js and project dependencies
3. **Version Check**: Verifies if the documentation version already exists
4. **Create Version**: Runs `npm run docusaurus docs:version <version>` to create versioned docs
5. **Verify**: Confirms all expected files were created:
   - `versioned_docs/version-<version>/` directory
   - `versioned_sidebars/version-<version>-sidebars.json` file
   - Updated `versions.json` file
6. **Create PR**: Creates a pull request with the versioned documentation changes

### Generated Files

When versioning documentation for `v0.6.0`, the workflow creates:

```
website/
├── versioned_docs/
│   └── version-0.6.x/          # Copy of current docs/ content
│       ├── getting-started/
│       ├── installation/
│       └── ...
├── versioned_sidebars/
│   └── version-0.6.x-sidebars.json  # Copy of current sidebar config
└── versions.json               # Updated with "0.6.x" entry
```

## Benefits

1. **Automatic**: No manual intervention required for versioning
2. **Consistent**: Uses the same versioning pattern every time
3. **Safe**: Creates a PR for review before merging changes
4. **Traceable**: Clear commit messages and PR descriptions
5. **Robust**: Includes verification steps and error handling

## Manual Usage

If you need to create versioned documentation manually:

```bash
cd website
npm run docusaurus docs:version 0.6.x
```

This will create the same files that the automated workflow creates.

## Configuration

The workflow is configured in `.github/workflows/docs-versioning.yaml` and uses:
- **Node.js 20.x**: For running Docusaurus commands
- **Yarn**: For dependency management (with caching)
- **GitHub CLI**: For creating pull requests

## Troubleshooting

### Workflow doesn't trigger
- Verify the tag follows the correct pattern (`v[0-9]+.[0-9]+.0`)
- Check that the tag was pushed (not just created locally)

### Version already exists
- The workflow checks if a version already exists and skips creation if found
- This prevents duplicate versioning attempts

### PR creation fails
- Ensure the GitHub token has sufficient permissions
- Check for branch naming conflicts

### Files not created
- The workflow includes verification steps that will fail if expected files aren't created
- Check the Docusaurus command output for errors

## Best Practices

1. **Review PRs**: Always review the generated PR before merging
2. **Test locally**: Test documentation builds locally before releasing
3. **Keep versions manageable**: Don't create too many versions (Docusaurus recommends <10)
4. **Update regularly**: Remove old versions when they're no longer supported

## Integration with Release Process

This workflow integrates with the existing release process:
1. Release workflow (`capz-release.yaml`) creates and pushes tags
2. Documentation versioning workflow (`docs-versioning.yaml`) detects new minor version tags
3. Documentation deployment workflow (`deploy-docs.yaml`) deploys the updated site

The workflows work together to ensure documentation stays in sync with releases.
