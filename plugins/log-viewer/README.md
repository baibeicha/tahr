# High-Throughput Log Viewer (`log-viewer`)

Fast, low-memory log viewer engineered to inspect multi-gigabyte log files without freezing Tahr IDE.

## Features
- **Chunked Memory-Mapped Engine**: Instant opening of huge log files (`.log`, `.out`, `.audit`) using indexed chunk offsets.
- **Severity Level Highlighting**: Automatic detection and color coding for `FATAL`, `ERROR`, `WARN`, `INFO`, `DEBUG`, and `TRACE`.
- **Live Real-time Tailing**: Follow file appends in real time (similar to `tail -f`) with auto-scroll and pause controls.
- **Regex & Substring Filter Pipeline**: Non-blocking asynchronous filtering across millions of lines.
- **Structured JSON & Timestamp Detection**: Collapsible view for JSON logs and formatted timestamp columns.
