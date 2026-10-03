# AI Chat Assistant (`ai-chat`)

Conversational AI programming assistant built directly into Tahr IDE.

## Features
- **Sidebar Chat Panel**: Press `Ctrl+L` or click `[AI]` in the Activity Bar to toggle the AI conversational panel.
- **Context Injection**:
  - `@file`: Injects active file or fuzzy-selected file into the context prompt.
  - `@selection`: Injects current highlighted code block.
  - `@diagnostics`: Automatically passes compiler/linter error messages from the Problems panel.
  - `@terminal`: Sends last command output or stack trace from the terminal drawer.
- **Interactive Code Actions**: Each code block rendered in the chat includes 1-click actions:
  - `[Apply to File]`: Atomically updates the target file using Myers diff.
  - `[Insert at Cursor]`: Places code block at active editor cursor position.
  - `[Copy]`: Copies snippet to system clipboard.
- **Provider Support**: Works out of the box with local `llama-server` (Zero-CGO) or Ollama, as well as private/cloud OpenAI-compatible endpoints.
