package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/dag"
	"tahr/internal/core/i18n"
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
	Kind           string
}

// NewDAGCanvasWidget creates a new canvas widget bound to a model and theme.
func NewDAGCanvasWidget(model *dag.GraphModel, theme *ui.Theme) *DAGCanvasWidget {
	if model == nil {
		model = dag.NewGraphModel()
	}
	needsLayout := false
	for _, n := range model.Nodes {
		if n.X == 0 && n.Y == 0 {
			needsLayout = true
			break
		}
	}
	if needsLayout && len(model.Nodes) > 0 {
		dag.LayoutSugiyama(model, dag.DefaultSugiyamaConfig())
	}
	router := dag.NewChannelRouter()
	router.RouteAll(model)

	widget := &DAGCanvasWidget{
		Model:  model,
		Router: router,
		Theme:  theme,
	}

	// Select first node deterministically
	var sortedIDs []string
	for id := range model.Nodes {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)
	if len(sortedIDs) > 0 {
		widget.SelectedNodeID = sortedIDs[0]
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
	sort.Strings(ids)
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
	sort.Strings(ids)
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

	// 1. Draw subtle background grid with safe positive modulus
	for y := bounds.Y; y < bounds.Y+bounds.Height; y++ {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			r := ' '
			relX := (x - bounds.X - cw.PanX)%12 + 12
			relY := (y - bounds.Y - cw.PanY)%6 + 6
			if relX%12 == 0 && relY%6 == 0 {
				r = '·'
			}
			buf.SetRune(x, y, r, edgeColor, bg, cell.AttrNone)
		}
	}

	// If no tables in model, render informative Empty State
	if len(cw.Model.Nodes) == 0 {
		cw.renderEmptyState(buf, bounds, fg, bg, edgeColor)
		return
	}

	// 2. Draw Node Cards deterministically (sorted by ID to prevent flicker/jitter)
	var nodeIDs []string
	for id := range cw.Model.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	for _, id := range nodeIDs {
		node := cw.Model.Nodes[id]
		scrX := bounds.X + node.X + cw.PanX
		scrY := bounds.Y + node.Y + cw.PanY
		isSelected := node.ID == cw.SelectedNodeID

		cardBorderColor := edgeColor
		cardTitleColor := fg
		cardAttr := cell.AttrNone
		if isSelected {
			cardBorderColor = activeColor
			cardTitleColor = activeColor
			cardAttr = cell.AttrBold
		}

		// Draw Frame: top, divider at y+1, bottom at y+h-1, side borders
		cw.renderCardFrame(buf, bounds, scrX, scrY, node.Width, node.Height, cardBorderColor, bg, cardAttr)

		// Title row (at scrY) with selection indicator
		title := fmt.Sprintf(" %s ", node.Title)
		if isSelected {
			title = fmt.Sprintf(" ▶ %s ", node.Title)
		}
		for i, r := range []rune(title) {
			cellX := scrX + 1 + i
			if cellX >= bounds.X && cellX < bounds.X+bounds.Width && cellX < scrX+node.Width-1 &&
				scrY >= bounds.Y && scrY < bounds.Y+bounds.Height {
				buf.SetRune(cellX, scrY, r, cardTitleColor, bg, cell.AttrBold)
			}
		}

		// Rows (starting at scrY + 2, after top border and separator line)
		for rIdx, row := range node.Rows {
			rowY := scrY + 2 + rIdx
			if rowY < bounds.Y || rowY >= bounds.Y+bounds.Height || rowY >= scrY+node.Height-1 {
				continue
			}

			// Key tag (3 chars)
			keyTag := "   "
			keyFg := fg
			if row.IsPK {
				keyTag = "PK "
				keyFg = pkColor
			} else if row.IsFK {
				keyTag = "FK "
				keyFg = fkColor
			}

			innerW := node.Width - 2 // space between left and right │
			if innerW < 6 {
				innerW = 6
			}
			nameW := innerW - 3 - len(row.DataType) - 1
			if nameW < 1 {
				nameW = 1
			}
			lineStr := fmt.Sprintf("%s%-*s %s", keyTag, nameW, row.Name, row.DataType)
			lineRunes := []rune(lineStr)
			if len(lineRunes) > innerW {
				lineRunes = lineRunes[:innerW]
			}

			for i, r := range lineRunes {
				cellX := scrX + 1 + i
				if cellX >= bounds.X && cellX < bounds.X+bounds.Width && cellX < scrX+node.Width-1 {
					rowFg := fg
					if i < 3 {
						rowFg = keyFg
					}
					buf.SetRune(cellX, rowY, r, rowFg, bg, cell.AttrNone)
				}
			}
		}
	}

	// 3. Draw Edges with Directional Grid (Corners, Junctions, Arrows)
	cw.renderEdges(buf, bounds, edgeColor, activeColor, bg)
}

// makeCardRow formats a row inside the empty state card to exactly cardW runes with borders.
func makeCardRow(text string, cardW int, center bool) string {
	text = strings.Trim(text, "│ ")
	r := []rune(text)
	innerW := cardW - 2
	if len(r) > innerW {
		r = r[:innerW]
	}
	if center {
		leftPad := (innerW - len(r)) / 2
		rightPad := innerW - len(r) - leftPad
		return "│" + strings.Repeat(" ", leftPad) + string(r) + strings.Repeat(" ", rightPad) + "│"
	}
	pad := innerW - len(r) - 2
	if pad < 0 {
		pad = 0
	}
	return "│  " + string(r) + strings.Repeat(" ", pad) + "│"
}

// renderEmptyState displays an informative empty card when no database or schema files exist.
func (cw *DAGCanvasWidget) renderEmptyState(buf *buffer.Buffer, bounds buffer.Rect, fg, bg, borderColor cell.Color) {
	cardW := 63
	borderTop := "╭" + strings.Repeat("─", cardW-2) + "╮"
	borderMid := "├" + strings.Repeat("─", cardW-2) + "┤"
	borderBtm := "╰" + strings.Repeat("─", cardW-2) + "╯"

	var lines []string
	if cw.Kind == "project-graph" || cw.Kind == "graph" {
		lines = []string{
			borderTop,
			makeCardRow(i18n.T("dag.project_graph_title"), cardW, true),
			borderMid,
			makeCardRow("", cardW, false),
			makeCardRow(i18n.T("dag.hint_open_code"), cardW, false),
			makeCardRow(i18n.T("dag.hint_f3_analysis"), cardW, false),
			makeCardRow("", cardW, false),
			makeCardRow(i18n.T("dag.modes_header"), cardW, false),
			makeCardRow(i18n.T("dag.mode_call_graph"), cardW, false),
			makeCardRow(i18n.T("dag.mode_blast_radius"), cardW, false),
			makeCardRow(i18n.T("dag.mode_modules"), cardW, false),
			makeCardRow("", cardW, false),
			borderBtm,
		}
	} else {
		lines = []string{
			borderTop,
			makeCardRow(i18n.T("dag.db_schema_not_found"), cardW, true),
			borderMid,
			makeCardRow("", cardW, false),
			makeCardRow(i18n.T("dag.db_hint_connect"), cardW, false),
			makeCardRow(i18n.T("dag.db_hint_add_sql"), cardW, false),
			makeCardRow("", cardW, false),
			makeCardRow(i18n.T("dag.db_supported"), cardW, false),
			makeCardRow("PostgreSQL, MySQL, MariaDB, SQLite, MSSQL,", cardW, false),
			makeCardRow("CockroachDB, DuckDB, ClickHouse, Redis", cardW, false),
			makeCardRow("", cardW, false),
			borderBtm,
		}
	}
	cardH := len(lines)
	startX := bounds.X + (bounds.Width-cardW)/2
	startY := bounds.Y + (bounds.Height-cardH)/2
	if startX < bounds.X {
		startX = bounds.X
	}
	if startY < bounds.Y {
		startY = bounds.Y
	}

	for rowIdx, line := range lines {
		y := startY + rowIdx
		if y >= bounds.Y+bounds.Height {
			break
		}
		runes := []rune(line)
		for colIdx, r := range runes {
			x := startX + colIdx
			if x >= bounds.X+bounds.Width {
				break
			}
			color := fg
			if rowIdx == 0 || rowIdx == 2 || rowIdx == len(lines)-1 || colIdx == 0 || colIdx == len(runes)-1 {
				color = borderColor
			} else if rowIdx == 1 {
				color = fg
			}
			attr := cell.AttrNone
			if rowIdx == 1 {
				attr = cell.AttrBold
			}
			buf.SetRune(x, y, r, color, bg, attr)
		}
	}
}

const (
	dirN uint8 = 1 << 0
	dirS uint8 = 1 << 1
	dirE uint8 = 1 << 2
	dirW uint8 = 1 << 3
)

type edgeGridCell struct {
	mask  uint8
	color cell.Color
}

func (cw *DAGCanvasWidget) renderEdges(buf *buffer.Buffer, bounds buffer.Rect, edgeColor, activeColor, bg cell.Color) {
	grid := make(map[[2]int]edgeGridCell)

	// Step A: Plot all edge segments into the directional grid
	for _, edge := range cw.Model.Edges {
		if len(edge.Points) < 2 {
			continue
		}
		isEdgeSelected := (edge.FromNode == cw.SelectedNodeID || edge.ToNode == cw.SelectedNodeID)
		eColor := edgeColor
		if isEdgeSelected {
			eColor = activeColor
		}

		for i := 0; i < len(edge.Points)-1; i++ {
			p1 := edge.Points[i]
			p2 := edge.Points[i+1]

			if p1[0] == p2[0] { // Vertical segment
				minY, maxY := p1[1], p2[1]
				if minY > maxY {
					minY, maxY = maxY, minY
				}
				x := p1[0]
				for y := minY; y <= maxY; y++ {
					pt := [2]int{x, y}
					cellVal := grid[pt]
					if y > minY {
						cellVal.mask |= dirN
					}
					if y < maxY {
						cellVal.mask |= dirS
					}
					if isEdgeSelected || cellVal.color.Value == 0 {
						cellVal.color = eColor
					}
					grid[pt] = cellVal
				}
			} else if p1[1] == p2[1] { // Horizontal segment
				minX, maxX := p1[0], p2[0]
				if minX > maxX {
					minX, maxX = maxX, minX
				}
				y := p1[1]
				for x := minX; x <= maxX; x++ {
					pt := [2]int{x, y}
					cellVal := grid[pt]
					if x > minX {
						cellVal.mask |= dirW
					}
					if x < maxX {
						cellVal.mask |= dirE
					}
					if isEdgeSelected || cellVal.color.Value == 0 {
						cellVal.color = eColor
					}
					grid[pt] = cellVal
				}
			}
		}
	}

	// Step B: Render directional runes with smooth corners and junctions
	for pt, cellVal := range grid {
		scrX := bounds.X + pt[0] + cw.PanX
		scrY := bounds.Y + pt[1] + cw.PanY
		if scrX < bounds.X || scrX >= bounds.X+bounds.Width || scrY < bounds.Y || scrY >= bounds.Y+bounds.Height {
			continue
		}

		var r rune
		switch cellVal.mask {
		case dirN | dirS:
			r = '│'
		case dirE | dirW:
			r = '─'
		case dirS | dirE:
			r = '╭'
		case dirS | dirW:
			r = '╮'
		case dirN | dirE:
			r = '╰'
		case dirN | dirW:
			r = '╯'
		case dirN | dirS | dirE:
			r = '├'
		case dirN | dirS | dirW:
			r = '┤'
		case dirE | dirW | dirS:
			r = '┬'
		case dirE | dirW | dirN:
			r = '┴'
		case dirN | dirS | dirE | dirW:
			r = '┼'
		case dirE, dirW:
			r = '─'
		case dirN, dirS:
			r = '│'
		default:
			if cellVal.mask&(dirE|dirW) != 0 && cellVal.mask&(dirN|dirS) != 0 {
				r = '┼'
			} else if cellVal.mask&(dirN|dirS) != 0 {
				r = '│'
			} else {
				r = '─'
			}
		}
		buf.SetRune(scrX, scrY, r, cellVal.color, bg, cell.AttrNone)
	}

	// Step C: Render arrow heads directly at edge destination
	for _, edge := range cw.Model.Edges {
		if len(edge.Points) < 2 {
			continue
		}
		lastPt := edge.Points[len(edge.Points)-1]
		scrX := bounds.X + lastPt[0] + cw.PanX
		scrY := bounds.Y + lastPt[1] + cw.PanY
		if scrX >= bounds.X && scrX < bounds.X+bounds.Width && scrY >= bounds.Y && scrY < bounds.Y+bounds.Height {
			markerRune := '▶'
			if edge.MarkerEnd == dag.MarkerCrowFootMany {
				markerRune = 'ᚚ'
			}
			markerColor := edgeColor
			if edge.FromNode == cw.SelectedNodeID || edge.ToNode == cw.SelectedNodeID {
				markerColor = activeColor
			}
			buf.SetRune(scrX, scrY, markerRune, markerColor, bg, cell.AttrBold)
		}
	}
}

func (cw *DAGCanvasWidget) renderCardFrame(buf *buffer.Buffer, b buffer.Rect, x, y, w, h int, fg, bg cell.Color, attr cell.Modifier) {
	// Fill card interior to occlude background grid and pass-through lines
	for cy := y; cy < y+h; cy++ {
		if cy >= b.Y && cy < b.Y+b.Height {
			for cx := x; cx < x+w; cx++ {
				if cx >= b.X && cx < b.X+b.Width {
					buf.SetRune(cx, cy, ' ', fg, bg, cell.AttrNone)
				}
			}
		}
	}

	for cx := x; cx < x+w; cx++ {
		if cx >= b.X && cx < b.X+b.Width {
			if y >= b.Y && y < b.Y+b.Height {
				buf.SetRune(cx, y, '─', fg, bg, attr)
			}
			if y+1 >= b.Y && y+1 < b.Y+b.Height {
				buf.SetRune(cx, y+1, '─', fg, bg, attr)
			}
			if y+h-1 >= b.Y && y+h-1 < b.Y+b.Height {
				buf.SetRune(cx, y+h-1, '─', fg, bg, attr)
			}
		}
	}
	for cy := y; cy < y+h; cy++ {
		if cy >= b.Y && cy < b.Y+b.Height {
			if x >= b.X && x < b.X+b.Width {
				buf.SetRune(x, cy, '│', fg, bg, attr)
			}
			if x+w-1 >= b.X && x+w-1 < b.X+b.Width {
				buf.SetRune(x+w-1, cy, '│', fg, bg, attr)
			}
		}
	}
	cw.setRuneSafe(buf, b, x, y, '╭', fg, bg, attr)
	cw.setRuneSafe(buf, b, x+w-1, y, '╮', fg, bg, attr)
	cw.setRuneSafe(buf, b, x, y+1, '├', fg, bg, attr)
	cw.setRuneSafe(buf, b, x+w-1, y+1, '┤', fg, bg, attr)
	cw.setRuneSafe(buf, b, x, y+h-1, '╰', fg, bg, attr)
	cw.setRuneSafe(buf, b, x+w-1, y+h-1, '╯', fg, bg, attr)
}

func (cw *DAGCanvasWidget) setRuneSafe(buf *buffer.Buffer, b buffer.Rect, x, y int, r rune, fg, bg cell.Color, attr cell.Modifier) {
	if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
		buf.SetRune(x, y, r, fg, bg, attr)
	}
}
