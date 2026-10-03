# ClickHouse Analytical Database Inspector (`clickhouse-inspector`)

High-performance visual inspector for ClickHouse columnar database management systems.

## Features
- **Table Engine Explorer**: Inspect MergeTree, ReplacingMergeTree, Replicated, and Distributed engine details.
- **Partitions & Compression Visualizer**: View part sizes, uncompressed vs compressed disk usage, and compression ratios.
- **Analytical DataGrid**: Execute analytical SQL queries with formatted columnar results and sub-millisecond execution benchmarks.
- **System Telemetry**: Real-time view of running queries (`system.processes`), mutations (`system.mutations`), and replication queue.
- **Pure-Go HTTP Engine**: Zero-CGO client communicating via ClickHouse HTTP protocol with streaming JSON parsing.
