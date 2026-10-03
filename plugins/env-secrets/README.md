# Env Secrets Conceal (`env-secrets`)

Safe environment variable and API secret visualization for Tahr IDE.

## Features
- **Conceal Masking**: Automatically masks values of passwords, API keys, and auth tokens in `.env` files with bullet dots (`••••••••`).
- **Cursor Reveal**: Placing the primary cursor on a concealed line temporarily reveals the raw value for editing.
- **Toggle Command**: Press `Ctrl+Shift+E` to toggle secret masking on or off for the active buffer.
- **Zero Buffer Mutation**: Powered by Tahr's Virtual Text Conceal Engine — only visual display is modified, underlying file bytes on disk are preserved with 100% accuracy.
