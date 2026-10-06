package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/jupyter"
	"tahr/internal/ui"
)

// JupyterButtonHit records interactive button coordinates.
type JupyterButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// JupyterCellHit records cell row coordinates.
type JupyterCellHit struct {
	Index int
	Y     int
	X, W  int
}

// JupyterPanel provides an interactive UI for executing and viewing Jupyter Notebooks.
type JupyterPanel struct {
	WorkspaceDir string
	Client       *jupyter.Client
	Notebook     *jupyter.Notebook
	FilePath     string
	Position     string // "right" or "bottom"
	Open         bool

	SelectedCellIdx int
	Offset          int
	Executing       bool

	ButtonHits []JupyterButtonHit
	CellHits   []JupyterCellHit

	mu sync.RWMutex

	OnToast func(level, title, msg string)
}

// NewJupyterPanel creates a new Jupyter notebook runner panel.
func NewJupyterPanel(workspaceDir string) *JupyterPanel {
	cfg := jupyter.DefaultClientConfig()
	cfg.WorkspaceDir = workspaceDir
	return &JupyterPanel{
		WorkspaceDir:    workspaceDir,
		Client:          jupyter.NewClient(cfg),
		Notebook:        jupyter.NewNotebook(),
		Position:        "right",
		Open:            true,
		SelectedCellIdx: 0,
	}
}

// LoadFile opens an existing .ipynb file.
func (p *JupyterPanel) LoadFile(path string) error {
	nb, err := jupyter.LoadNotebook(path)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.Notebook = nb
	p.FilePath = path
	p.SelectedCellIdx = 0
	p.Offset = 0
	p.mu.Unlock()
	return nil
}

// RunSelectedCell executes the currently highlighted cell.
func (p *JupyterPanel) RunSelectedCell() {
	p.mu.Lock()
	if p.Executing || p.Notebook == nil || p.SelectedCellIdx < 0 || p.SelectedCellIdx >= len(p.Notebook.Cells) {
		p.mu.Unlock()
		return
	}
	p.Executing = true
	targetCell := &p.Notebook.Cells[p.SelectedCellIdx]
	client := p.Client
	p.mu.Unlock()

	go func() {
		err := client.ExecuteCell(context.Background(), targetCell)
		p.mu.Lock()
		p.Executing = false
		p.mu.Unlock()

		if err != nil && p.OnToast != nil {
			p.OnToast("error", "JUPYTER", "Execution failed: "+err.Error())
		}
	}()
}

// RunAll executes all code cells in sequential order.
func (p *JupyterPanel) RunAll() {
	p.mu.Lock()
	if p.Executing || p.Notebook == nil {
		p.mu.Unlock()
		return
	}
	p.Executing = true
	client := p.Client
	p.mu.Unlock()

	go func() {
		p.mu.Lock()
		cellCount := len(p.Notebook.Cells)
		p.mu.Unlock()

		for i := 0; i < cellCount; i++ {
			p.mu.Lock()
			if i >= len(p.Notebook.Cells) {
				p.mu.Unlock()
				break
			}
			cellPtr := &p.Notebook.Cells[i]
			isCode := cellPtr.CellType == jupyter.CellTypeCode
			p.mu.Unlock()

			if isCode {
				_ = client.ExecuteCell(context.Background(), cellPtr)
			}
		}
		p.mu.Lock()
		p.Executing = false
		p.mu.Unlock()

		if p.OnToast != nil {
			p.OnToast("info", "JUPYTER", "Finished executing all cells")
		}
	}()
}

// AddCell appends a new empty cell of the given type.
func (p *JupyterPanel) AddCell(cType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Notebook == nil {
		p.Notebook = jupyter.NewNotebook()
	}
	_ = p.Notebook.AddCell(cType, "")
	p.SelectedCellIdx = len(p.Notebook.Cells) - 1
}

// ClearOutputs clears output buffers across all cells.
func (p *JupyterPanel) ClearOutputs() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Notebook != nil {
		for i := range p.Notebook.Cells {
			p.Notebook.Cells[i].ClearOutputs()
		}
	}
}

// Save persists changes back to file.
func (p *JupyterPanel) Save() {
	p.mu.RLock()
	filePath := p.FilePath
	nb := p.Notebook
	p.mu.RUnlock()

	if filePath != "" && nb != nil {
		if err := nb.Save(filePath); err != nil && p.OnToast != nil {
			p.OnToast("error", "JUPYTER", "Save error: "+err.Error())
		} else if p.OnToast != nil {
			p.OnToast("info", "JUPYTER", "Notebook saved")
		}
	}
}

// HandleKey processes keyboard navigation and execution shortcuts.
func (p *JupyterPanel) HandleKey(k input.Key) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Notebook == nil || len(p.Notebook.Cells) == 0 {
		return false
	}

	switch k.Type {
	case input.KeyUp:
		if p.SelectedCellIdx > 0 {
			p.SelectedCellIdx--
		}
		return true

	case input.KeyDown:
		if p.SelectedCellIdx < len(p.Notebook.Cells)-1 {
			p.SelectedCellIdx++
		}
		return true

	case input.KeyEnter:
		p.mu.Unlock()
		p.RunSelectedCell()
		p.mu.Lock()
		return true
	}

	return false
}

// HandleClick processes button clicks and cell selections.
func (p *JupyterPanel) HandleClick(x, y int) bool {
	p.mu.RLock()
	btnHits := append([]JupyterButtonHit{}, p.ButtonHits...)
	cellHits := append([]JupyterCellHit{}, p.CellHits...)
	p.mu.RUnlock()

	for _, hit := range btnHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "run_cell":
				p.RunSelectedCell()
			case "run_all":
				p.RunAll()
			case "add_code":
				p.AddCell(jupyter.CellTypeCode)
			case "add_md":
				p.AddCell(jupyter.CellTypeMarkdown)
			case "clear":
				p.ClearOutputs()
			case "save":
				p.Save()
			}
			return true
		}
	}

	for _, hit := range cellHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			p.mu.Lock()
			p.SelectedCellIdx = hit.Index
			p.mu.Unlock()
			return true
		}
	}

	return false
}

// RenderRect renders the Jupyter panel in rectangular bounds.
func (p *JupyterPanel) RenderRect(buf *buffer.Buffer, bounds buffer.Rect, theme *ui.Theme) {
	p.Render(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, theme)
}

// Render draws the interactive Jupyter Notebook viewer.
func (p *JupyterPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme *ui.Theme) {
	if w <= 0 || h <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ButtonHits = p.ButtonHits[:0]
	p.CellHits = p.CellHits[:0]

	bg := toColor(theme.StatusBarBg)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	selBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	warnFg := toColor(theme.DiagnosticWarn)
	greenFg := toColor(theme.String)

	// Backdrop
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	curY := y

	// 1. Header Bar: Kernel + Status
	kStatus := "○ idle"
	kColor := greenFg
	if p.Executing {
		kStatus = "● running"
		kColor = warnFg
	}
	kInfo := fmt.Sprintf("Kernel: Python 3  │  %s", kStatus)
	if p.FilePath != "" {
		kInfo = fmt.Sprintf("%s  │  %s", filepath.Base(p.FilePath), kStatus)
	}
	drawText(buf, x+1, curY, kInfo, kColor, bg, cell.AttrBold)
	curY++

	// 2. Action buttons (with wrapping for narrow sidebars)
	buttons := []struct {
		Label  string
		Action string
	}{
		{" Run Cell ", "run_cell"},
		{" Run All ", "run_all"},
		{" + Code ", "add_code"},
		{" + Text ", "add_md"},
		{" Clear ", "clear"},
		{" Save ", "save"},
	}
	btnX := x + 1
	for _, b := range buttons {
		bLen := len([]rune(b.Label))
		if btnX+bLen >= x+w-1 && btnX > x+1 {
			curY++
			btnX = x + 1
		}
		drawText(buf, btnX, curY, b.Label, textFg, toColor(theme.CursorLineBg), cell.AttrBold)
		p.ButtonHits = append(p.ButtonHits, JupyterButtonHit{
			Action: b.Action,
			X:      btnX,
			Y:      curY,
			Width:  bLen,
		})
		btnX += bLen + 1
	}
	curY++

	// 3. Cells List
	if p.Notebook == nil || len(p.Notebook.Cells) == 0 {
		curY++
		emptyTitle := i18n.T("jupyter.empty_title")
		emptyDesc := i18n.T("jupyter.empty_desc")
		emptyLines := []string{
			emptyTitle,
			"───────────────────────────────",
			emptyDesc,
			i18n.T("jupyter.hint_code"),
			i18n.T("jupyter.hint_text"),
			i18n.T("jupyter.hint_run"),
			"",
			i18n.T("jupyter.empty_footer"),
		}
		for _, el := range emptyLines {
			if curY >= y+h-1 {
				break
			}
			c := dimFg
			attr := cell.AttrNone
			if el == emptyTitle {
				c = warnFg
				attr = cell.AttrBold
			} else if el == emptyDesc {
				c = accentFg
			} else if strings.HasPrefix(el, "•") {
				c = textFg
			}
			drawText(buf, x+1, curY, el, c, bg, attr)
			curY++
		}
		return
	}

	listH := y + h - curY
	curRow := 0

	for i := p.Offset; i < len(p.Notebook.Cells) && curRow < listH; i++ {
		c := p.Notebook.Cells[i]
		cellY := curY + curRow
		isSelected := i == p.SelectedCellIdx

		rowBg := bg
		rowFg := textFg
		prefix := "  "
		if isSelected {
			rowBg = selBg
			prefix = "● "
		}

		// Cell header line: [In: 1] code or [MD]
		headerTag := ""
		if c.CellType == jupyter.CellTypeCode {
			if c.ExecutionCount != nil {
				headerTag = fmt.Sprintf("In [%d]:", *c.ExecutionCount)
			} else {
				headerTag = "In [ ]:"
			}
		} else {
			headerTag = "Markdown:"
		}

		sourcePreview := strings.TrimSpace(c.GetSource())
		if idx := strings.Index(sourcePreview, "\n"); idx != -1 {
			sourcePreview = sourcePreview[:idx] + " ..."
		}
		if len(sourcePreview) > w-len(headerTag)-6 {
			sourcePreview = sourcePreview[:w-len(headerTag)-6]
		}

		cellHeader := fmt.Sprintf("%s%s %s", prefix, headerTag, sourcePreview)
		drawText(buf, x+1, cellY, cellHeader, rowFg, rowBg, cell.AttrBold)
		p.CellHits = append(p.CellHits, JupyterCellHit{
			Index: i,
			Y:     cellY,
			X:     x + 1,
			W:     w - 2,
		})
		curRow++

		// Render output lines if any
		if len(c.Outputs) > 0 && curRow < listH {
			for _, out := range c.Outputs {
				if curRow >= listH {
					break
				}
				outText := strings.TrimSpace(out.TextContent())
				if outText == "" {
					continue
				}
				outLines := strings.Split(outText, "\n")
				outFg := greenFg
				if out.OutputType == jupyter.OutputTypeError {
					outFg = errFg
				}

				for _, l := range outLines {
					if curRow >= listH {
						break
					}
					if len(l) > w-6 {
						l = l[:w-6]
					}
					drawText(buf, x+5, curY+curRow, "│ "+l, outFg, bg, cell.AttrNone)
					curRow++
				}
			}
		}

		curRow++ // blank line between cells
	}
}
