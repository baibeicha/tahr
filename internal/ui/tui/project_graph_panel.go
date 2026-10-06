package tui

import (
	"fmt"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/dag"
	"tahr/internal/ui"
)

// GraphViewMode selects which project graph is actively rendered in the split.
type GraphViewMode int

const (
	ModeCallHierarchyDownstream GraphViewMode = 0
	ModeCallHierarchyUpstream   GraphViewMode = 1
	ModeModuleImports           GraphViewMode = 2
	ModeRouteToCode             GraphViewMode = 3
)

// ProjectGraphPanel coordinates the interactive graph view within an editor split pane.
type ProjectGraphPanel struct {
	Mode         GraphViewMode
	Canvas       *DAGCanvasWidget
	FocusRadius  int // 1 or 2
	TargetSymbol string
	Theme        *ui.Theme
}

// NewProjectGraphPanel initializes a graph panel widget.
func NewProjectGraphPanel(theme *ui.Theme) *ProjectGraphPanel {
	model := dag.NewGraphModel()
	return &ProjectGraphPanel{
		Mode:        ModeCallHierarchyDownstream,
		Canvas:      NewDAGCanvasWidget(model, theme),
		FocusRadius: 1,
		Theme:       theme,
	}
}

// SetModel updates the active graph model, calculates Sugiyama layout, and re-routes edges.
func (p *ProjectGraphPanel) SetModel(model *dag.GraphModel) {
	if model == nil {
		model = dag.NewGraphModel()
	}
	dag.LayoutSugiyama(model, dag.DefaultSugiyamaConfig())
	p.Canvas = NewDAGCanvasWidget(model, p.Theme)
}

// CycleMode advances to the next graph visualization mode.
func (p *ProjectGraphPanel) CycleMode() GraphViewMode {
	p.Mode = (p.Mode + 1) % 4
	return p.Mode
}

// ModeTitle returns a clean label for the current graph mode.
func (p *ProjectGraphPanel) ModeTitle() string {
	switch p.Mode {
	case ModeCallHierarchyDownstream:
		return "Call Graph (Downstream)"
	case ModeCallHierarchyUpstream:
		return "Blast Radius (Upstream)"
	case ModeModuleImports:
		return "Module Imports & Cycles"
	case ModeRouteToCode:
		return "Route-to-Code Pipeline"
	default:
		return "Project Graph"
	}
}

// Render draws the graph panel with top mode bar and interactive DAG canvas.
func (p *ProjectGraphPanel) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	fg := toColor(p.Theme.Foreground)
	activeFg := toColor(p.Theme.Function)
	hdrBg := toColor(p.Theme.SelectionBg)

	// 1. Draw Mini Mode Header Bar
	headerText := fmt.Sprintf(" [Mode: %s]  (Ring R=%d)  [r: Switch Mode] [Enter: Jump Code] ", p.ModeTitle(), p.FocusRadius)
	for x := bounds.X; x < bounds.X+bounds.Width; x++ {
		r := ' '
		tFg := fg
		idx := x - bounds.X
		if idx < len(headerText) {
			r = rune(headerText[idx])
			tFg = activeFg
		}
		buf.SetRune(x, bounds.Y, r, tFg, hdrBg, cell.AttrBold)
	}

	// 2. Render Canvas Widget below header
	if bounds.Height > 1 {
		canvasBounds := buffer.NewRect(bounds.X, bounds.Y+1, bounds.Width, bounds.Height-1)
		p.Canvas.Render(buf, canvasBounds)
	}
}
