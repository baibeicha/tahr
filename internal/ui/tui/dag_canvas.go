package tui

import (
	"fmt"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/dag"
	"tahr/internal/ui"
)

// DAGCanvasWidget renders an interactive DAG graph with pan/zoom and card manipulation.
type DAGCanvasWidget struct {
	Model          *dag.GraphModel
	Router         *dag.ChannelRouter
	PanX           int
	PanY           int
	SelectedNodeID string
	SelectedPortID string
	Theme          *ui.Theme
}

// NewDAGCanvasWidget creates a new canvas widget bound to a model and theme.
func NewDAGCanvasWidget(model *dag.GraphModel, theme *ui.Theme) *DAGCanvasWidget {
	if model == nil {
		model = dag.NewGraphModel()
	}
	router := dag.NewChannelRouter()
	router.RouteAll(model)

	widget := &DAGCanvasWidget{
		Model:  model,
		Router: router,
		Theme:  theme,
	}
	// Select first node by default if available
	for id := range model.Nodes {
		widget.SelectedNodeID = id
		break
	}
	return widget
}

// Pan shifts the canvas viewport by dx and dy.
func (cw *DAGCanvasWidget) Pan(dx, dy int) {
	cw.PanX += dx
	cw.PanY += dy
}

// SelectNextNode cycles selection to the next node.
func (cw *DAGCanvasWidget) SelectNextNode() {
	var ids []string
	for id := range cw.Model.Nodes {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	for i, id := range ids {
		if id == cw.SelectedNodeID {
			cw.SelectedNodeID = ids[(i+1)%len(ids)]
			return
		}
	}
	cw.SelectedNodeID = ids[0]
}

// SelectPrevNode cycles selection to the previous node.
func (cw *DAGCanvasWidget) SelectPrevNode() {
	var ids []string
	for id := range cw.Model.Nodes {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	for i, id := range ids {
		if id == cw.SelectedNodeID {
			cw.SelectedNodeID = ids[(i-1+len(ids))%len(ids)]
			return
		}
	}
	cw.SelectedNodeID = ids[len(ids)-1]
}

// MoveSelectedNode moves the currently selected card and incrementally re-routes incident edges.
func (cw *DAGCanvasWidget) MoveSelectedNode(dx, dy int) {
	if cw.SelectedNodeID == "" {
		return
	}
	node := cw.Model.Nodes[cw.SelectedNodeID]
	if node != nil {
		node.X += dx
		node.Y += dy
		cw.Router.RouteIncremental(cw.Model, cw.SelectedNodeID)
	}
}

// Render draws the complete canvas (edges, cards, ports, selection) into bounds.
func (cw *DAGCanvasWidget) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	fg := toColor(cw.Theme.Foreground)
	bg := toColor(cw.Theme.Background)
	edgeColor := toColor(cw.Theme.LineNumber)
	activeColor := toColor(cw.Theme.Function)
	pkColor := toColor(0xF6C177) // Gold PK
	fkColor := toColor(0x9CCFD8) // Cyan FK

	// 1. Draw subtle background grid
	for y := bounds.Y; y < bounds.Y+bounds.Height; y++ {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			r := ' '
			if (x-cw.PanX)%10 == 0 && (y-cw.PanY)%5 == 0 {
				r = '·'
			}
			buf.SetRune(x, y, r, edgeColor, bg, cell.AttrNone)
		}
	}

	// 2. Draw Edges
	for _, edge := range cw.Model.Edges {
		if len(edge.Points) < 2 {
			continue
		}
		for i := 0; i < len(edge.Points)-1; i++ {
			p1 := edge.Points[i]
			p2 := edge.Points[i+1]
			cw.drawSegment(buf, bounds, p1, p2, edgeColor, bg)
		}
		// Draw end marker
		lastPt := edge.Points[len(edge.Points)-1]
		scrX := bounds.X + lastPt[0] + cw.PanX
		scrY := bounds.Y + lastPt[1] + cw.PanY
		if scrX >= bounds.X && scrX < bounds.X+bounds.Width && scrY >= bounds.Y && scrY < bounds.Y+bounds.Height {
			markerRune := '▶'
			if edge.MarkerEnd == dag.MarkerCrowFootMany {
				markerRune = 'ᚚ'
			}
			buf.SetRune(scrX, scrY, markerRune, activeColor, bg, cell.AttrBold)
		}
	}

	// 3. Draw Node Cards
	for _, node := range cw.Model.Nodes {
		scrX := bounds.X + node.X + cw.PanX
		scrY := bounds.Y + node.Y + cw.PanY
		isSelected := node.ID == cw.SelectedNodeID

		cardBorderColor := edgeColor
		cardTitleColor := fg
		if isSelected {
			cardBorderColor = activeColor
			cardTitleColor = activeColor
		}

		// Draw Frame
		cw.renderCardFrame(buf, bounds, scrX, scrY, node.Width, node.Height, cardBorderColor, bg)

		// Title
		title := fmt.Sprintf(" %s ", node.Title)
		for i, r := range []rune(title) {
			if scrX+2+i < bounds.X+bounds.Width && scrX+2+i >= bounds.X && scrY >= bounds.Y && scrY < bounds.Y+bounds.Height {
				buf.SetRune(scrX+2+i, scrY, r, cardTitleColor, bg, cell.AttrBold)
			}
		}

		// Rows
		for rIdx, row := range node.Rows {
			rowY := scrY + 2 + rIdx
			if rowY < bounds.Y || rowY >= bounds.Y+bounds.Height {
				continue
			}

			// Key icon
			keyTag := "   "
			keyFg := fg
			if row.IsPK {
				keyTag = "PK "
				keyFg = pkColor
			} else if row.IsFK {
				keyTag = "FK "
				keyFg = fkColor
			}

			lineStr := fmt.Sprintf("%s%-*s %s", keyTag, node.Width-len(row.DataType)-8, row.Name, row.DataType)
			for i, r := range []rune(lineStr) {
				cellX := scrX + 1 + i
				if cellX >= bounds.X && cellX < bounds.X+bounds.Width && cellX < scrX+node.Width-1 {
					rowFg := fg
					if i < 3 {
						rowFg = keyFg
					}
					buf.SetRune(cellX, rowY, r, rowFg, bg, cell.AttrNone)
				}
			}

			// Draw Port Anchors on left/right borders
			if scrX >= bounds.X && scrX < bounds.X+bounds.Width {
				buf.SetRune(scrX, rowY, '○', cardBorderColor, bg, cell.AttrNone)
			}
			rightBorder := scrX + node.Width - 1
			if rightBorder >= bounds.X && rightBorder < bounds.X+bounds.Width {
				buf.SetRune(rightBorder, rowY, '●', cardBorderColor, bg, cell.AttrNone)
			}
		}
	}
}

func (cw *DAGCanvasWidget) drawSegment(buf *buffer.Buffer, b buffer.Rect, p1, p2 [2]int, fg, bg cell.Color) {
	x1 := b.X + p1[0] + cw.PanX
	y1 := b.Y + p1[1] + cw.PanY
	x2 := b.X + p2[0] + cw.PanX
	y2 := b.Y + p2[1] + cw.PanY

	if x1 == x2 { // Vertical segment
		minY, maxY := y1, y2
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		if x1 >= b.X && x1 < b.X+b.Width {
			for y := minY; y <= maxY; y++ {
				if y >= b.Y && y < b.Y+b.Height {
					buf.SetRune(x1, y, '│', fg, bg, cell.AttrNone)
				}
			}
		}
	} else if y1 == y2 { // Horizontal segment
		minX, maxX := x1, x2
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if y1 >= b.Y && y1 < b.Y+b.Height {
			for x := minX; x <= maxX; x++ {
				if x >= b.X && x < b.X+b.Width {
					buf.SetRune(x, y1, '─', fg, bg, cell.AttrNone)
				}
			}
		}
	}
}

func (cw *DAGCanvasWidget) renderCardFrame(buf *buffer.Buffer, b buffer.Rect, x, y, w, h int, fg, bg cell.Color) {
	for cx := x; cx < x+w; cx++ {
		if cx >= b.X && cx < b.X+b.Width {
			if y >= b.Y && y < b.Y+b.Height {
				buf.SetRune(cx, y, '─', fg, bg, cell.AttrNone)
			}
			if y+1 >= b.Y && y+1 < b.Y+b.Height {
				buf.SetRune(cx, y+1, '─', fg, bg, cell.AttrNone)
			}
			if y+h-1 >= b.Y && y+h-1 < b.Y+b.Height {
				buf.SetRune(cx, y+h-1, '─', fg, bg, cell.AttrNone)
			}
		}
	}
	for cy := y; cy < y+h; cy++ {
		if cy >= b.Y && cy < b.Y+b.Height {
			if x >= b.X && x < b.X+b.Width {
				buf.SetRune(x, cy, '│', fg, bg, cell.AttrNone)
			}
			if x+w-1 >= b.X && x+w-1 < b.X+b.Width {
				buf.SetRune(x+w-1, cy, '│', fg, bg, cell.AttrNone)
			}
		}
	}
	cw.setRuneSafe(buf, b, x, y, '╭', fg, bg)
	cw.setRuneSafe(buf, b, x+w-1, y, '╮', fg, bg)
	cw.setRuneSafe(buf, b, x, y+1, '├', fg, bg)
	cw.setRuneSafe(buf, b, x+w-1, y+1, '┤', fg, bg)
	cw.setRuneSafe(buf, b, x, y+h-1, '╰', fg, bg)
	cw.setRuneSafe(buf, b, x+w-1, y+h-1, '╯', fg, bg)
}

func (cw *DAGCanvasWidget) setRuneSafe(buf *buffer.Buffer, b buffer.Rect, x, y int, r rune, fg, bg cell.Color) {
	if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
		buf.SetRune(x, y, r, fg, bg, cell.AttrNone)
	}
}
