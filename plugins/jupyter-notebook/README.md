# Jupyter Notebook Interactive Runner (`jupyter-notebook`)

Native, interactive `.ipynb` Jupyter notebook runner and viewer for Tahr IDE.

## Features
- **Hybrid Execution Engine**: Auto-spawns a local `ipykernel` / Python subprocess or connects to a running remote Jupyter Server.
- **Rich MIME-Type Rendering**: Seamless inline rendering of plain text, HTML tables, Markdown documentation, and PNG/SVG data visualizations.
- **Interactive Cell Shortcuts**: Execute cell and advance (`Shift+Enter`), execute all cells (`Ctrl+Shift+Enter`), or interrupt long-running computations.
- **Kernel Management**: Easily switch kernels (Python 3, Gophernotes, IRkernel), restart kernel, or interrupt execution with real-time cell state indicators (`idle`, `busy`, `queued`).
- **Zero-CGO Pure Go Parser**: Native JSON serialization compliant with Jupyter Notebook Format v4.
