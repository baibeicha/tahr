# Database Inspector (`db-inspector`)

Universal, zero-CGO database client and schema inspector for Tahr IDE supporting PostgreSQL, MySQL, MariaDB, SQLite, MSSQL, CockroachDB, DuckDB, ClickHouse, and Redis.

## Features
- **Pure-Go Architecture**: Pure Go wire protocol drivers without native C client dependencies.
- **Tree Schema Explorer**: Inspect databases, schemas, tables, columns, indexes, foreign keys, and constraints.
- **Virtualized DataGrid**: Browse table rows with sorting, inline filtering, pagination, and JSON column formatting.
- **Interactive Query Console**: Execute queries directly from `.sql` files (`Ctrl+Enter`) with syntax highlighting, timing, and affected rows count.
- **Environment Autodetection**: Automatically discovers credentials from `DATABASE_URL`, `POSTGRES_URL`, and workspace `.env` files.
