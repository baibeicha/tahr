# SQLite Database Viewer (`sqlite-viewer`)

Instant, Zero-CGO SQLite database visualizer and query tool in Tahr IDE.

## Features
- **Zero-Config Inspection**: Click on any `.db`, `.sqlite`, or `.sqlite3` file in the file explorer to open it in a rich DataGrid tab.
- **Table & Schema Navigation**: View all tables, views, column data types, primary keys, and indexes in the sidebar panel (`[DB]`).
- **High-Performance DataGrid**: Virtualized rendering supporting large tables with paging, column sorting, and cell selection.
- **SQL Console**: Execute arbitrary queries (`SELECT * FROM users WHERE active = 1`) and inspect results with duration metrics.
- **Zero-CGO Pure Go**: Pure Go binary reader first for instant reads, with automatic fallback to `sqlite3` CLI for heavy writes.
