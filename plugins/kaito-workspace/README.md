# kaito-workspace Plugin

A Copilot CLI plugin that helps you deploy LLM models to Kubernetes using the KAITO kubectl plugin.

## Quick install

```bash
# Via marketplace
copilot plugin marketplace add kaito-project/kaito
copilot plugin install kaito-workspace@kaito-skills

# Or directly
copilot plugin install kaito-project/kaito:plugins/kaito-workspace
```

## What's included

- **kaito-inference skill** — Guides you through model selection, GPU sizing, and generates ready-to-run `kubectl kaito deploy` commands.

## Plugin structure

```
plugins/kaito-workspace/
├── README.md                          # This file
└── skills/
    └── kaito-inference/
        └── SKILL.md                   # Skill instructions
```

## Managing the plugin

```bash
# Update to latest version
copilot plugin update kaito-workspace

# Temporarily disable
copilot plugin disable kaito-workspace

# Re-enable
copilot plugin enable kaito-workspace

# Uninstall
copilot plugin uninstall kaito-workspace
```
