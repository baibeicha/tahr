<div align="center">

# ⚡ Tahr IDE

**Modern, Blazing-Fast Terminal & Graphical IDE built in Go**

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-blue)](#installation)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Release](https://img.shields.io/badge/Release-v0.1.0-blueviolet)](https://github.com/baibeicha/tahr/releases)

*High-Performance Augmented Rope Buffer • Multi-Cursor • DAP Debugger • Zstandard & WASM Plugins • Multi-Language i18n*

</div>

---

## 🌟 Overview

**Tahr** is a next-generation terminal and graphical IDE engineered from the ground up in pure Go. It delivers sub-millisecond editor responsiveness, ultra-low memory overhead, seamless multi-pane layout splits, and a modern plugin ecosystem powered by compressed Zstandard packages and WebAssembly sandboxing.

---

## 🚀 Key Highlights

* **Augmented Rope Buffer**: $O(\log N)$ text inserts, deletes, and splits capable of opening gigabyte-sized files instantaneously without UI thread stalls.
* **Universal Multi-Language Test Runner**: Automated test discovery and execution for Go, Rust, Python, JavaScript/TypeScript, Java (Maven/Gradle), PHP (Pest/PHPUnit), C# (.NET), Kotlin, Zig, and Ruby with real-time test tree reporting.
* **Peer-to-Peer Real-Time Collaboration**: Distributed pair programming with multi-tier signaling cascades (mDNS LAN, Nostr WebSockets, BitTorrent DHT), follower mode, and permission-gated terminal sharing.
* **Full-Featured LSP & DAP**: Out-of-the-box Language Server Protocol (gopls, pyright, rust-analyzer, clangd, typescript-language-server, jdtls, intelephense, omnisharp, zls, solargraph) and Delve DAP interactive debugging (Call Stack, Variables, Breakpoints, Step Over/Into/Out).
* **Dynamic Split Panes (1 to 6 Views)**: Single, 2-column, 3-column, 4-grid (2x2), 5-pane, and 6-grid (3x2) editor splits with instant hotkey cycling (`Ctrl+\`).
* **Multi-Cursor & Smart Selections**: Full multi-cursor capabilities (`Ctrl+D`, `Alt+Click`), bracket rainbow matching, and word selections.
* **Integrated Terminal Drawer**: TrueColor terminal tabs (`F4` / `Ctrl+~`) powered by Windows ConPTY and POSIX PTY with live resize handles.
* **Zstandard Plugin Ecosystem**: Modular extensions packaged into compact `.tahr` archives with a built-in Extension Marketplace (`Ctrl+,`). Every plugin bundles its own internal localized catalogs.
* **Full Multi-Language Localization**: English base UI with dynamic language pack loading (including Russian `tahr-ru`, Chinese `tahr-zh`, and Spanish `tahr-es`).
* **Instant Startup & Reliability**: Asynchronous SDK probe pipeline (<10ms startup time), defer/recover panic crash recording, and one-click diagnostic bug reporter.

---

## 📦 Installation & Quick Start

### Option 1: Pre-built Binaries (Recommended)

Download the latest release for your platform from [GitHub Releases](https://github.com/baibeicha/tahr/releases):

* **Windows**: `tahr-windows-amd64.zip`
* **Linux**: `tahr-linux-amd64.tar.gz`
* **macOS (Apple Silicon)**: `tahr-darwin-arm64.tar.gz`
* **macOS (Intel)**: `tahr-darwin-amd64.tar.gz`

Extract and place `tahr` into your `PATH`.

### Option 2: Build from Source

```bash
# Clone the repository
git clone https://github.com/baibeicha/tahr.git
cd tahr

# Build the Tahr binary
go build -o bin/tahr ./cmd/tahr

# Run Tahr
./bin/tahr
```

---

## ⌨️ Essential Keybindings

| Shortcut | Description |
|---|---|
| `F5` / `Ctrl+R` | Run active project or file profile |
| `F7` / `Ctrl+Shift+B` | Build active project |
| `F2` / `Ctrl+B` | Toggle Project Tree file sidebar |
| `Ctrl+Alt+E` | Dock Project Tree (Left ⟷ Right) |
| `Ctrl+\` | Cycle Split Layout (1 to 6 panes) |
| `Alt+1..6` | Switch focus between Split Panes 1..6 |
| `F4` / `Ctrl+~` | Toggle Integrated Terminal drawer |
| `F8` | Toggle DAP Debugger HUD |
| `F9` | Toggle Breakpoint |
| `F10` / `F11` | Step Over / Step Into line |
| `Ctrl+Shift+T` | Toggle Universal Test Explorer |
| `Ctrl+Shift+L` | Toggle Peer-to-Peer Collaboration panel |
| `Ctrl+,` | Open IDE Settings & Extension Marketplace |
| `Ctrl+P` | Omnibar: Rapid fuzzy file search |
| `Ctrl+Shift+P` | Command Palette (Actions, Tools, Formatting) |
| `Ctrl+F` | Find & Replace in file |
| `Ctrl+Z` / `Ctrl+Y` | Multi-level Undo / Redo |
| `Ctrl+Shift+G` | Interactive Git hunk staging & graph visualizer |
| `Ctrl+Q` | Quit editor |

---

## 🧩 Plugin Ecosystem & Registry

Tahr features an official registry containing 36+ production-grade extensions:

* **Official Language Packs**: `tahr-ru` (Russian), `tahr-zh` (Simplified Chinese), `tahr-es` (Spanish)
* **Language Support**: `tahr-go`, `tahr-rust`, `tahr-python`, `tahr-ts`, `tahr-clangd`, `tahr-java`, `tahr-php`, `tahr-kotlin`, `tahr-zig`, `tahr-csharp`, `tahr-ruby`
* **Database & Storage**: `sqlite-viewer`, `db-inspector`, `db-er-diagram`, `clickhouse-inspector`, `redis-inspector`, `s3-viewer`
* **Cloud & DevOps**: `docker-compose`, `k8s-inspector`, `tahr-kafka`, `tahr-nats`, `tahr-rabbitmq`, `remote-ssh`
* **AI & Developer Productivity**: `ai-chat`, `ai-completion`, `rest-client`, `regex-tester`, `profiler`, `jupyter-notebook`, `test-runner`, `test-coverage`

Each plugin encapsulates its own internal translations (`locales/en.json`, `locales/ru.json`, `locales/zh.json`, `locales/es.json`) to keep the core IDE lean and fully decoupled.

### Developing & Packaging a Plugin

```bash
# 1. Scaffold a new plugin project
tahr plugin init my-plugin --lang=go

# 2. Package into a .tahr Zstandard archive
tahr plugin pack ./my-plugin ./plugins/my-plugin.tahr

# 3. Install locally into user configuration
tahr plugin install ./plugins/my-plugin.tahr
```

---

## 🏗️ Architecture

```
tahr/
├── cmd/
│   ├── tahr/               # Main CLI, TUI and GUI entrypoints
│   └── tahr-pack/          # Plugin packaging utility
├── internal/
│   ├── core/
│   │   ├── buffer/         # Augmented Rope data structure
│   │   ├── crash/          # Panic recovery & crash dump recorder
│   │   ├── dap/            # Debug Adapter Protocol client
│   │   ├── i18n/           # Internationalization & locale registry
│   │   ├── logging/        # Non-blocking ring-buffer logger
│   │   ├── lsp/            # Language Server Protocol client
│   │   ├── plugin/         # Plugin discovery, WASM host & registry
│   │   ├── sdk/            # Toolchain and compiler auto-detector
│   │   └── syntax/         # Tree-sitter AST highlighter
│   └── ui/
│       ├── tui/            # GoatUI terminal frontend & modal dialogs
│       └── gui/            # Gio graphical interface (optional)
├── plugins/                # Bundled extensions & official registry.json
└── pkg/
    └── tahr_sdk/           # External Go SDK for plugin authors
```

---

## 📄 License

MIT License. Designed and developed with ❤️ for high-performance engineering.
