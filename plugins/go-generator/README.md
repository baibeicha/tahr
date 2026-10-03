# Go Code & Tag Generator (`go-generator`)

High-speed AST-driven boilerplate code generation for Go developers in Tahr IDE.

## Features
- **Struct Tags (`Alt+Enter`)**:
  - Automatically adds `json:"field_name" db:"field_name"` to struct definitions.
  - Supports configurable casing (`snake_case`, `camelCase`).
  - Supports custom tags (`yaml`, `xml`, `validate`, `gorm`).
- **Constructor Generator**:
  - Automatically writes `func NewStructName(...) *StructName` with parameter initialization.
- **Interface Implementation (`impl`)**:
  - Generates receiver method stubs required by any interface (e.g. `io.Reader`, `http.Handler`, or custom interfaces).
- **Zero-CGO**: Pure Go parser and formatter via standard library `go/parser`, `go/ast`, and `go/format`.
