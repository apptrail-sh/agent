# Cursor Rules

Organized rules for AppTrail Controller.

## Structure

```
.cursor/rules/
├── 00-project-context.md      # Architecture, tech stack, key patterns
├── 01-code-patterns.md        # Reconciliation, metrics, notifiers, gotchas
└── 02-testing-deployment.md   # Testing framework, commands, config
```

## Philosophy

These rules focus on **project-specific patterns** that an AI needs to write correct code:
- Critical gotchas (metric cardinality, registration)
- Architecture patterns (channel-based notifications)
- Project-specific conventions (version labels, notifier interface)

Generic Go/Kubernetes knowledge is omitted - AI assistants already know that.

## Adding Rules

Keep it focused:
- ✅ Project-specific patterns
- ✅ Critical gotchas
- ✅ Code examples for common tasks
- ❌ Generic advice
- ❌ Documentation that belongs in README
- ❌ Command lists (use `make help`)
