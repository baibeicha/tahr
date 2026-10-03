# Docker Compose Manager (`docker-compose`)

Comprehensive container orchestration and management in Tahr IDE.

## Features
- **Compose Service Discovery**: Automatically detects `docker-compose.yml`, `compose.yaml`, and `.env` in the active workspace.
- **Service Status TreeView**: Visual status badges (`running`, `exited`, `restarting`, `healthy`) with port forwardings in the sidebar (`[Docker]`).
- **One-Click Lifecycle Actions**: Start (`Up`), stop (`Down`), rebuild, and restart services from the tool window.
- **Log Streaming**: Stream live multi-container color-coded logs into a dedicated terminal split pane.
- **Direct Shell Exec**: Open an interactive `sh` or `bash` session inside any running container in the terminal drawer.
- **Zero-CGO High Performance**: Direct HTTP-over-socket client first (Unix socket on Linux/macOS, Named Pipe on Windows), with automatic fallback to `docker compose` CLI.
