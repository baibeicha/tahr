# Universal Test Runner (`test-runner`)

Multi-language test discoverer and runner for Tahr IDE.

## Features
- **CodeLens Run / Debug**: Interactive buttons above test functions to run or debug single tests directly from the editor.
- **Language Coverage**:
  - Go: `go test -v -run <test>`
  - Rust: `cargo test <test>`
  - Python: `pytest -k <test>`
  - TypeScript/JavaScript: `vitest` / `jest`
  - C/C++: `ctest`
- **Output Panel**: Clean formatted output with pass/fail counts and stack traces.
