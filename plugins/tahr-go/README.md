# Go Language Support for Tahr IDE (`tahr-go`)

Official language extension providing comprehensive tooling for Go development in Tahr IDE.

## Features
- **Language Server Protocol (LSP)**: Powered by `gopls` for intelligent code completion, go-to-definition (F12), find references (Shift+F12), hover documentation, and real-time compilation diagnostics.
- **Debug Adapter Protocol (DAP)**: Full interactive debugging via Delve (`dlv dap`). Set breakpoints, step through code, inspect variables and stack frames.
- **Go Toolchain Autodetection**: Automatically discovers Go SDK from `GOROOT`, `GOPATH`, or PATH.
- **Project Scaffolding**: 1-click templates for CLI tools, REST APIs, and Go packages.
