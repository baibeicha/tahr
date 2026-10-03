# Workspace TODO Tree (`todo-tree`)

Project-wide task and code comment organizer for Tahr IDE.

## Features
- **Tag Recognition**: Automatically detects `TODO:`, `FIXME:`, `BUG:`, `HACK:`, `NOTE:`, and `PERF:` annotations across all source files.
- **Hierarchical TreeView**: Displays comments grouped by file or by tag type in the left sidebar tool window (`[Todo]`).
- **Instant Navigation**: Click or press Enter on any item to jump directly to the exact file and line number.
- **Incremental Updates**: Uses Tahr's fast directory scanner and Rope in-memory buffer index.
