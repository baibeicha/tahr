# AI Inline Code Completion (`ai-completion`)

Next-generation AI code completion providing ghost text suggestions directly inside the buffer.

## Features
- **Ghost Text Inlay**: Displays inline greyed-out AI completions ahead of the cursor; press `Tab` to accept, `Esc` to dismiss.
- **Local LLM First**: Native zero-latency integration with local `llama-server` or `ollama`.
- **Custom Provider Support**: Configurable with any OpenAI-compatible API endpoint (local or cloud) by setting `endpoint` and `api_key` in plugin settings.
