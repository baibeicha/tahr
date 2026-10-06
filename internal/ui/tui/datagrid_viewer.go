package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/db"
	"tahr/internal/ui"
)

// DataGridWidget provides an interactive tabular table viewer similar to JetBrains DataGrip.
type DataGridWidget struct {
	TableName    string
	Columns      []db.Column
	FKColumns    map[string]bool
	AllRows      [][]string
	FilteredRows [][]string
	Page         int
	PageSize     int
	SelectedRow  int
	FilterText   string
	FilterActive bool
	Theme        *ui.Theme
}

// NewDataGridWidget creates and initializes a DataGrid viewer for a given table schema.
func NewDataGridWidget(tableName string, table *db.Table, theme *ui.Theme) *DataGridWidget {
	cols := make([]db.Column, 0)
	fkCols := make(map[string]bool)
	if table != nil && len(table.Columns) > 0 {
		cols = append(cols, table.Columns...)
		for _, fk := range table.ForeignKeys {
			fkCols[fk.FromColumn] = true
		}
	} else {
		// Fallback schema for empty / ad-hoc result sets
		cols = []db.Column{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "name", DataType: "varchar(100)", IsNullable: false},
			{Name: "status", DataType: "varchar(50)", IsNullable: false},
			{Name: "created_at", DataType: "timestamp", IsNullable: false},
		}
	}

	rows := generateSampleRows(tableName, cols, 48)

	dg := &DataGridWidget{
		TableName:    tableName,
		Columns:      cols,
		FKColumns:    fkCols,
		AllRows:      rows,
		FilteredRows: rows,
		Page:         0,
		PageSize:     16,
		SelectedRow:  0,
		Theme:        theme,
	}
	return dg
}

// generateSampleRows synthesizes realistic relational table rows for schema columns.
func generateSampleRows(table string, cols []db.Column, count int) [][]string {
	rows := make([][]string, 0, count)
	for i := 1; i <= count; i++ {
		row := make([]string, len(cols))
		for cIdx, col := range cols {
			colLower := strings.ToLower(col.Name)
			typeLower := strings.ToLower(col.DataType)

			if col.IsPK || strings.Contains(typeLower, "uuid") {
				row[cIdx] = fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i)
			} else if strings.Contains(colLower, "email") {
				names := []string{"alice", "bob", "charlie", "david", "emma", "fiona", "george"}
				name := names[(i-1)%len(names)]
				row[cIdx] = fmt.Sprintf("%s%d@example.com", name, i)
			} else if strings.Contains(colLower, "name") || strings.Contains(colLower, "title") {
				names := []string{"Alpha Service", "Beta Pipeline", "Gamma Gateway", "Delta Queue", "Epsilon Storage"}
				row[cIdx] = fmt.Sprintf("%s %d", names[(i-1)%len(names)], i)
			} else if strings.Contains(colLower, "price") || strings.Contains(colLower, "amount") || strings.Contains(colLower, "total") || strings.Contains(typeLower, "numeric") || strings.Contains(typeLower, "decimal") {
				row[cIdx] = fmt.Sprintf("%.2f", float64(i)*19.99+4.50)
			} else if strings.Contains(typeLower, "int") || strings.Contains(typeLower, "serial") {
				row[cIdx] = fmt.Sprintf("%d", i*10)
			} else if strings.Contains(typeLower, "bool") {
				if i%2 == 0 {
					row[cIdx] = "true"
				} else {
					row[cIdx] = "false"
				}
			} else if strings.Contains(typeLower, "time") || strings.Contains(typeLower, "date") {
				row[cIdx] = fmt.Sprintf("2026-10-%02d 10:%02d:00", (i%28)+1, (i*3)%60)
			} else if strings.Contains(colLower, "status") {
				statuses := []string{"ACTIVE", "PENDING", "COMPLETED", "ARCHIVED"}
				row[cIdx] = statuses[(i-1)%len(statuses)]
			} else {
				row[cIdx] = fmt.Sprintf("%s_%d", col.Name, i)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// NextPage advances to the next page of rows.
func (dg *DataGridWidget) NextPage() {
	totalPages := dg.TotalPages()
	if dg.Page < totalPages-1 {
		dg.Page++
		dg.SelectedRow = 0
	}
}

// PrevPage goes back to the previous page of rows.
func (dg *DataGridWidget) PrevPage() {
	if dg.Page > 0 {
		dg.Page--
		dg.SelectedRow = 0
	}
}

// TotalPages returns total number of pages based on PageSize.
func (dg *DataGridWidget) TotalPages() int {
	tot := len(dg.FilteredRows)
	if tot == 0 {
		return 1
	}
	pages := tot / dg.PageSize
	if tot%dg.PageSize != 0 {
		pages++
	}
	return pages
}

// SelectNextRow moves row selection down.
func (dg *DataGridWidget) SelectNextRow() {
	pageRows := dg.CurrentPageRows()
	if dg.SelectedRow < len(pageRows)-1 {
		dg.SelectedRow++
	}
}

// SelectPrevRow moves row selection up.
func (dg *DataGridWidget) SelectPrevRow() {
	if dg.SelectedRow > 0 {
		dg.SelectedRow--
	}
}

// CurrentPageRows returns the slice of rows visible on the current page.
func (dg *DataGridWidget) CurrentPageRows() [][]string {
	tot := len(dg.FilteredRows)
	if tot == 0 {
		return nil
	}
	start := dg.Page * dg.PageSize
	if start >= tot {
		start = 0
		dg.Page = 0
	}
	end := start + dg.PageSize
	if end > tot {
		end = tot
	}
	return dg.FilteredRows[start:end]
}

// SetFilter filters table rows matching text across all columns.
func (dg *DataGridWidget) SetFilter(query string) {
	dg.FilterText = query
	dg.Page = 0
	dg.SelectedRow = 0
	if strings.TrimSpace(query) == "" {
		dg.FilteredRows = dg.AllRows
		return
	}
	q := strings.ToLower(strings.TrimSpace(query))
	filtered := make([][]string, 0)
	for _, row := range dg.AllRows {
		matched := false
		for _, val := range row {
			if strings.Contains(strings.ToLower(val), q) {
				matched = true
				break
			}
		}
		if matched {
			filtered = append(filtered, row)
		}
	}
	dg.FilteredRows = filtered
}

// ExportCSV exports rows in CSV format and optionally writes them to workspaceDir/<tableName>_export.csv.
func (dg *DataGridWidget) ExportCSV(workspaceDir ...string) string {
	var sb strings.Builder
	// Header
	var colHeaders []string
	for _, col := range dg.Columns {
		colHeaders = append(colHeaders, fmt.Sprintf("%q", col.Name))
	}
	sb.WriteString(strings.Join(colHeaders, ",") + "\n")
	// Rows
	for _, row := range dg.FilteredRows {
		var cells []string
		for _, cell := range row {
			cells = append(cells, fmt.Sprintf("%q", cell))
		}
		sb.WriteString(strings.Join(cells, ",") + "\n")
	}
	content := sb.String()
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		outPath := filepath.Join(workspaceDir[0], fmt.Sprintf("%s_export.csv", dg.TableName))
		_ = os.WriteFile(outPath, []byte(content), 0644)
	}
	return content
}

// Render draws the complete DataGrid into bounds.
func (dg *DataGridWidget) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	fg := toColor(dg.Theme.Foreground)
	bg := toColor(dg.Theme.Background)
	borderFg := toColor(dg.Theme.BorderColor)
	hdrBg := toColor(dg.Theme.GutterBg)
	hdrFg := toColor(dg.Theme.Function)
	accentFg := toColor(dg.Theme.Keyword)
	pkColor := toColor(0xF6C177) // Gold PK
	fkColor := toColor(0x9CCFD8) // Cyan FK
	selBg := toColor(dg.Theme.PopupSelBg)
	selFg := toColor(dg.Theme.PopupSelFg)

	// Fill background
	for y := bounds.Y; y < bounds.Y+bounds.Height; y++ {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			buf.SetRune(x, y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// 1. Top Control Bar (Row 0)
	totalPages := dg.TotalPages()
	startRow := dg.Page*dg.PageSize + 1
	endRow := min(len(dg.FilteredRows), (dg.Page+1)*dg.PageSize)
	if len(dg.FilteredRows) == 0 {
		startRow = 0
		endRow = 0
	}

	topBar := fmt.Sprintf(" Таблица: %s │ Строки: %d..%d из %d │ Стр. %d/%d │  ◀ Пред    След ▶    ⟳ Обновить    Export CSV ",
		dg.TableName, startRow, endRow, len(dg.FilteredRows), dg.Page+1, totalPages)
	for i, r := range []rune(topBar) {
		if bounds.X+i < bounds.X+bounds.Width {
			rColor := fg
			attr := cell.AttrNone
			if strings.ContainsRune("Таблица:", r) || i < 12 {
				rColor = accentFg
				attr = cell.AttrBold
			} else if strings.ContainsRune("◀ПредСлед▶", r) {
				rColor = hdrFg
			} else if strings.ContainsRune("Export CSV", r) {
				rColor = pkColor
			}
			buf.SetRune(bounds.X+i, bounds.Y, r, rColor, hdrBg, attr)
		}
	}
	// Fill rest of top bar
	for x := bounds.X + len([]rune(topBar)); x < bounds.X+bounds.Width; x++ {
		buf.SetRune(x, bounds.Y, ' ', fg, hdrBg, cell.AttrNone)
	}

	// Divider below top bar (Row 1)
	if bounds.Height > 1 {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			buf.SetRune(x, bounds.Y+1, '─', borderFg, bg, cell.AttrNone)
		}
	}

	// 2. Column Headers (Row 2)
	colCount := len(dg.Columns)
	if colCount == 0 {
		return
	}
	// Column # for row numbers
	rowNumColW := 5
	availW := bounds.Width - rowNumColW - (colCount + 1)
	if availW < 10 {
		availW = 10
	}
	colW := availW / colCount
	if colW < 8 {
		colW = 8
	}

	hdrY := bounds.Y + 2
	if hdrY < bounds.Y+bounds.Height {
		// Draw row number header
		buf.SetRune(bounds.X, hdrY, '│', borderFg, hdrBg, cell.AttrNone)
		rNumHdr := "   # "
		for i, r := range []rune(rNumHdr) {
			if bounds.X+1+i < bounds.X+rowNumColW {
				buf.SetRune(bounds.X+1+i, hdrY, r, toColor(dg.Theme.LineNumber), hdrBg, cell.AttrBold)
			}
		}
		curX := bounds.X + rowNumColW
		buf.SetRune(curX, hdrY, '│', borderFg, hdrBg, cell.AttrNone)
		curX++

		for _, col := range dg.Columns {
			tag := ""
			tagColor := hdrFg
			if col.IsPK {
				tag = " [PK]"
				tagColor = pkColor
			} else if dg.FKColumns != nil && dg.FKColumns[col.Name] {
				tag = " [FK]"
				tagColor = fkColor
			}
			title := fmt.Sprintf(" %s%s (%s)", col.Name, tag, col.DataType)
			for i, r := range []rune(title) {
				if curX+i < curX+colW && curX+i < bounds.X+bounds.Width {
					buf.SetRune(curX+i, hdrY, r, tagColor, hdrBg, cell.AttrBold)
				}
			}
			curX += colW
			if curX < bounds.X+bounds.Width {
				buf.SetRune(curX, hdrY, '│', borderFg, hdrBg, cell.AttrNone)
				curX++
			}
		}
	}

	// Divider below column headers (Row 3)
	divY := bounds.Y + 3
	if divY < bounds.Y+bounds.Height {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			buf.SetRune(x, divY, '─', borderFg, bg, cell.AttrNone)
		}
		// Intersections
		buf.SetRune(bounds.X, divY, '├', borderFg, bg, cell.AttrNone)
		buf.SetRune(bounds.X+rowNumColW, divY, '┼', borderFg, bg, cell.AttrNone)
		curX := bounds.X + rowNumColW + 1
		for cIdx := 0; cIdx < colCount; cIdx++ {
			curX += colW
			if curX < bounds.X+bounds.Width {
				buf.SetRune(curX, divY, '┼', borderFg, bg, cell.AttrNone)
				curX++
			}
		}
	}

	// 3. Data Rows (starting Row 4)
	pageRows := dg.CurrentPageRows()
	dataStartY := bounds.Y + 4
	maxDataY := bounds.Y + bounds.Height - 2

	for rIdx, row := range pageRows {
		rowY := dataStartY + rIdx
		if rowY > maxDataY {
			break
		}
		isSel := (rIdx == dg.SelectedRow)
		rowBg := bg
		rowFg := fg
		if isSel {
			rowBg = selBg
			rowFg = selFg
		}

		// Row index cell
		buf.SetRune(bounds.X, rowY, '│', borderFg, rowBg, cell.AttrNone)
		rNumStr := fmt.Sprintf("%4d ", dg.Page*dg.PageSize+rIdx+1)
		for i, r := range []rune(rNumStr) {
			if bounds.X+1+i < bounds.X+rowNumColW {
				buf.SetRune(bounds.X+1+i, rowY, r, toColor(dg.Theme.LineNumber), rowBg, cell.AttrNone)
			}
		}
		curX := bounds.X + rowNumColW
		buf.SetRune(curX, rowY, '│', borderFg, rowBg, cell.AttrNone)
		curX++

		for cIdx := 0; cIdx < colCount; cIdx++ {
			val := ""
			if cIdx < len(row) {
				val = row[cIdx]
			}
			cellText := fmt.Sprintf(" %-*s", colW-1, val)
			for i, r := range []rune(cellText) {
				if curX+i < curX+colW && curX+i < bounds.X+bounds.Width {
					buf.SetRune(curX+i, rowY, r, rowFg, rowBg, cell.AttrNone)
				}
			}
			curX += colW
			if curX < bounds.X+bounds.Width {
				buf.SetRune(curX, rowY, '│', borderFg, rowBg, cell.AttrNone)
				curX++
			}
		}
	}

	// 4. Bottom Footer Bar (Row bounds.Y + bounds.Height - 1)
	footY := bounds.Y + bounds.Height - 1
	if footY >= bounds.Y {
		filterLabel := " Поиск: "
		if dg.FilterText != "" {
			filterLabel += dg.FilterText
		} else {
			filterLabel += "введите текст..."
		}

		footText := fmt.Sprintf(" %s │ Навигация: ↑↓ строки, PgUp/PgDn страницы │ Экспорт: Export CSV", filterLabel)
		for i, r := range []rune(footText) {
			if bounds.X+i < bounds.X+bounds.Width {
				rColor := toColor(dg.Theme.LineNumber)
				if strings.ContainsRune("Поиск:", r) {
					rColor = accentFg
				}
				buf.SetRune(bounds.X+i, footY, r, rColor, hdrBg, cell.AttrNone)
			}
		}
		for x := bounds.X + len([]rune(footText)); x < bounds.X+bounds.Width; x++ {
			buf.SetRune(x, footY, ' ', fg, hdrBg, cell.AttrNone)
		}
	}
}
