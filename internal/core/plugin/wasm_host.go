package plugin

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// EditorHost allows the WASM sandbox to query and modify editor state safely.
type EditorHost interface {
	GetTotalLines() int
	GetLine(line int) ([]byte, error)
	InsertText(line, col int, text string) error
	DeleteRange(startLine, startCol, endLine, endCol int) error
	GetCursor() (line, col int)
	SetCursor(line, col int) error
	Save() error
	Log(msg string)
}

// WASMHost manages the Wazero runtime and sandbox isolation.
type WASMHost struct {
	mu     sync.RWMutex
	ctx    context.Context
	rt     wazero.Runtime
	editor EditorHost
	logs   []string
}

// NewWASMHost creates a new pure-Go Wazero WebAssembly host.
func NewWASMHost() (*WASMHost, error) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)

	// Instantiate WASI preview 1
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("instantiate WASI: %w", err)
	}

	return &WASMHost{
		ctx:  ctx,
		rt:   r,
		logs: make([]string, 0),
	}, nil
}

// SetEditorHost binds the active editor instance for WASM plugin interactions.
func (h *WASMHost) SetEditorHost(ed EditorHost) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.editor = ed
}

// Logs returns all captured plugin log messages.
func (h *WASMHost) Logs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]string(nil), h.logs...)
}

// Close terminates the Wazero runtime.
func (h *WASMHost) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rt != nil {
		return h.rt.Close(h.ctx)
	}
	return nil
}

// ExecuteGuest loads and runs a WASM module with capability enforcement and watchdog timeout.
func (h *WASMHost) ExecuteGuest(
	wasmBytes []byte,
	manifest *Manifest,
	functionName string,
	input []byte,
	timeout time.Duration,
) ([]byte, error) {
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}

	callCtx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()

	// Build host module exporting capabilities and full editor ABI
	builder := h.rt.NewHostModuleBuilder("env")

	// 1. Export tahr_ui_log
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) {
			bytes, ok := m.Memory().Read(ptr, size)
			if ok {
				msg := string(bytes)
				h.mu.Lock()
				h.logs = append(h.logs, msg)
				ed := h.editor
				h.mu.Unlock()
				if ed != nil {
					ed.Log(msg)
				}
			}
		}).
		Export("tahr_ui_log")

	// 2. Export tahr_buffer_get_total_lines
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 0
			}
			return uint32(ed.GetTotalLines())
		}).
		Export("tahr_buffer_get_total_lines")

	// 3. Export tahr_buffer_get_line
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, line, outPtr, maxLen uint32) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 0xFFFFFFFF
			}
			lineBytes, err := ed.GetLine(int(line))
			if err != nil {
				return 0xFFFFFFFF
			}
			copyLen := uint32(len(lineBytes))
			if copyLen > maxLen {
				copyLen = maxLen
			}
			if copyLen > 0 {
				m.Memory().Write(outPtr, lineBytes[:copyLen])
			}
			return copyLen
		}).
		Export("tahr_buffer_get_line")

	// 4. Export tahr_buffer_insert_text
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, line, col, ptr, size uint32) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 1
			}
			textBytes, ok := m.Memory().Read(ptr, size)
			if !ok {
				return 2
			}
			if err := ed.InsertText(int(line), int(col), string(textBytes)); err != nil {
				return 3
			}
			return 0
		}).
		Export("tahr_buffer_insert_text")

	// 5. Export tahr_buffer_delete_range
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, startLine, startCol, endLine, endCol uint32) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 1
			}
			if err := ed.DeleteRange(int(startLine), int(startCol), int(endLine), int(endCol)); err != nil {
				return 2
			}
			return 0
		}).
		Export("tahr_buffer_delete_range")

	// 6. Export tahr_buffer_get_cursor
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, outPtr uint32) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 1
			}
			line, col := ed.GetCursor()
			coords := make([]byte, 8)
			binary.LittleEndian.PutUint32(coords[0:4], uint32(line))
			binary.LittleEndian.PutUint32(coords[4:8], uint32(col))
			if !m.Memory().Write(outPtr, coords) {
				return 2
			}
			return 0
		}).
		Export("tahr_buffer_get_cursor")

	// 7. Export tahr_buffer_set_cursor
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, line, col uint32) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 1
			}
			if err := ed.SetCursor(int(line), int(col)); err != nil {
				return 2
			}
			return 0
		}).
		Export("tahr_buffer_set_cursor")

	// 8. Export tahr_buffer_save
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module) uint32 {
			h.mu.RLock()
			ed := h.editor
			h.mu.RUnlock()
			if ed == nil {
				return 1
			}
			if err := ed.Save(); err != nil {
				return 2
			}
			return 0
		}).
		Export("tahr_buffer_save")

	// 9. Export tahr_sys_exec (Capability-gated)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, cmdPtr, cmdLen uint32) uint32 {
			if manifest == nil || !manifest.HasCapability("process:exec") {
				return 1 // Permission denied
			}
			cmdBytes, ok := m.Memory().Read(cmdPtr, cmdLen)
			if !ok {
				return 2
			}
			cmdStr := string(cmdBytes)

			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("cmd.exe", "/c", cmdStr)
			} else {
				cmd = exec.Command("sh", "-c", cmdStr)
			}
			if err := cmd.Run(); err != nil {
				return 3
			}
			return 0
		}).
		Export("tahr_sys_exec")

	if _, err := builder.Instantiate(callCtx); err != nil {
		// Ignore if already instantiated
	}

	mod, err := h.rt.Instantiate(callCtx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("instantiate WASM module: %w", err)
	}
	defer mod.Close(callCtx)

	fn := mod.ExportedFunction(functionName)
	if fn == nil {
		fn = mod.ExportedFunction("_start")
		if fn == nil {
			return nil, fmt.Errorf("function %s not exported", functionName)
		}
	}

	_, err = fn.Call(callCtx)
	if err != nil {
		return nil, fmt.Errorf("wasm invocation: %w", err)
	}

	return []byte("ok"), nil
}
