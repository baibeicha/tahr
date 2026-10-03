# Universal Task Runner (`task-runner`)

Automatic task discovery and runner for project build workflows in Tahr IDE.

## Features
- **Auto-Discovery**: Scans project root for:
  - `Makefile`: Targets like `build`, `test`, `clean`, `deploy`.
  - `Taskfile.yml`: Task groups and descriptions.
  - `package.json`: NPM / Bun / Yarn scripts.
  - `Justfile`: Just command runner recipes.
  - `deno.json`: Deno tasks.
- **Terminal Integration**: Executes chosen tasks directly in Tahr's integrated PTY terminal drawer with colored streaming output and keyboard interaction.
- **Keybindings**:
  - `Ctrl+Shift+T`: Open task selection modal or focus Tasks panel.
  - `Ctrl+Alt+T`: Re-run last executed task.
