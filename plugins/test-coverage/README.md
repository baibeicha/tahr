# Test Coverage Visualizer (`test-coverage`)

Visualizes unit test coverage directly on your code in Tahr IDE.

## Features
- **Gutter Indicators**: Green glyphs for lines covered by tests, red glyphs for missed lines.
- **Coverage Summary**: Status bar and panel metrics showing percentage of statements/lines covered.
- **Supported Formats**:
  - Go: `coverage.out` (`go test -coverprofile=...`)
  - Generic: `lcov.info` (Jest, Vitest, Rust tarpaulin, gcov)
