package tui

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/media"
	"tahr/internal/core"
	"tahr/internal/ui"
)

// HoverDocState manages the hover delay timer, active documentation popup, and local AST comment fallback.
type HoverDocState struct {
	Open        bool
	ScreenX     int
	ScreenY     int
	Symbol      string
	Signature   string
	DocLines    []string
	Source      string // "LSP", "Code Comments", "Image Preview"
	Thumbnail   image.Image
	ThumbFormat string

	PendingWord string
	PendingLine int
	PendingCol  int
	PendingX    int
	PendingY    int
	TriggerTime time.Time
}

// NewHoverDocState initializes empty hover doc state.
func NewHoverDocState() *HoverDocState {
	return &HoverDocState{}
}

// Start initiates a debounced hover check for the given symbol under mouse.
func (h *HoverDocState) Start(word string, docLine, docCol, screenX, screenY int, delay time.Duration) {
	if word == "" {
		h.Dismiss()
		return
	}
	// If already pending or displaying this exact symbol and position, keep existing trigger
	if h.PendingWord == word && h.PendingLine == docLine && (h.Open || !h.TriggerTime.IsZero()) {
		return
	}

	h.PendingWord = word
	h.PendingLine = docLine
	h.PendingCol = docCol
	h.PendingX = screenX
	h.PendingY = screenY
	h.TriggerTime = time.Now().Add(delay)
	h.Open = false
}

// Dismiss immediately closes the hover popup and cancels pending trigger.
func (h *HoverDocState) Dismiss() {
	h.Open = false
	h.PendingWord = ""
	h.PendingLine = -1
	h.PendingCol = -1
	h.TriggerTime = time.Time{}
	h.Thumbnail = nil
}

// Render draws the minimal floating documentation popup on top of the editor buffer.
func (h *HoverDocState) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !h.Open || buf == nil || (h.Signature == "" && len(h.DocLines) == 0 && h.Thumbnail == nil) {
		return
	}

	bg := toColor(theme.PopupBg)
	borderFg := toColor(theme.BorderColor)
	titleFg := toColor(theme.Function)
	sigFg := toColor(theme.Keyword)
	docFg := toColor(theme.Foreground)

	// Compute required width
	maxContentW := len(h.Symbol) + 12
	if len(h.Signature) > maxContentW {
		maxContentW = len(h.Signature)
	}
	for _, l := range h.DocLines {
		if len(l) > maxContentW {
			maxContentW = len(l)
		}
	}
	if h.Thumbnail != nil && maxContentW < 32 {
		maxContentW = 32
	}

	popW := min(max(34, maxContentW+4), min(70, screenW-4))

	// Compute required height
	popH := 2 // top and bottom border
	if h.Signature != "" {
		popH += 1
	}
	if len(h.DocLines) > 0 {
		if h.Signature != "" {
			popH += 1 // divider line
		}
		popH += min(8, len(h.DocLines))
	}
	if h.Thumbnail != nil {
		popH += 8 // 16x8 thumbnail area
	}
	popH = min(popH, screenH-4)

	// Calculate top-left position (default below cursor, or above if close to bottom)
	popX := h.ScreenX
	if popX+popW >= screenW-1 {
		popX = max(1, screenW-popW-1)
	}

	popY := h.ScreenY + 1
	if popY+popH >= screenH-2 {
		popY = max(2, h.ScreenY-popH)
	}

	// 1. Fill background and draw borders
	for y := popY; y < popY+popH; y++ {
		for x := popX; x < popX+popW; x++ {
			buf.Set(x, y, cell.Cell{
				Rune:     ' ',
				Width:    1,
				FgType:   cell.ColorRGB,
				Fg:       docFg.Value,
				BgType:   cell.ColorRGB,
				Bg:       bg.Value,
				Modifier: cell.AttrNone,
			})
		}
	}

	// Borders
	box := DefaultBoxChars()
	// Top border
	buf.SetRune(popX, popY, box.TopLeft, borderFg, bg, cell.AttrNone)
	for x := popX + 1; x < popX+popW-1; x++ {
		buf.SetRune(x, popY, box.Horiz, borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(popX+popW-1, popY, box.TopRight, borderFg, bg, cell.AttrNone)

	// Bottom border
	buf.SetRune(popX, popY+popH-1, box.BottomLeft, borderFg, bg, cell.AttrNone)
	for x := popX + 1; x < popX+popW-1; x++ {
		buf.SetRune(x, popY+popH-1, box.Horiz, borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(popX+popW-1, popY+popH-1, box.BottomRight, borderFg, bg, cell.AttrNone)

	// Side borders
	for y := popY + 1; y < popY+popH-1; y++ {
		buf.SetRune(popX, y, box.Vert, borderFg, bg, cell.AttrNone)
		buf.SetRune(popX+popW-1, y, box.Vert, borderFg, bg, cell.AttrNone)
	}

	// Title in top border
	title := fmt.Sprintf(" %s ", h.Symbol)
	if h.Source != "" {
		title = fmt.Sprintf(" %s [%s] ", h.Symbol, h.Source)
	}
	tRunes := []rune(title)
	for i, r := range tRunes {
		if popX+2+i < popX+popW-2 {
			buf.SetRune(popX+2+i, popY, r, titleFg, bg, cell.AttrBold)
		}
	}

	// 2. Render Content
	contentY := popY + 1

	// Signature line
	if h.Signature != "" && contentY < popY+popH-1 {
		sigText := h.Signature
		if len(sigText) > popW-4 {
			sigText = sigText[:popW-7] + "..."
		}
		buf.SetString(popX+2, contentY, sigText, sigFg, bg, cell.AttrBold)
		contentY++

		// Divider
		if (len(h.DocLines) > 0 || h.Thumbnail != nil) && contentY < popY+popH-1 {
			for x := popX + 1; x < popX+popW-1; x++ {
				buf.SetRune(x, contentY, '─', borderFg, bg, cell.AttrNone)
			}
			contentY++
		}
	}

	// Thumbnail preview if hovering over image file
	if h.Thumbnail != nil && contentY+6 < popY+popH {
		thumbW := min(24, popW-4)
		thumbH := min(6, popY+popH-1-contentY)
		thumbRect := buffer.NewRect(popX+2, contentY, thumbW, thumbH)
		media.RenderHalfBlock(buf, thumbRect, h.Thumbnail, media.ScaleFit)
		contentY += thumbH
	}

	// Documentation comments lines
	for _, line := range h.DocLines {
		if contentY >= popY+popH-1 {
			break
		}
		txt := line
		if len(txt) > popW-4 {
			txt = txt[:popW-7] + "..."
		}
		buf.SetString(popX+2, contentY, txt, docFg, bg, cell.AttrNone)
		contentY++
	}
}

// ExtractLocalSymbolDocumentation scans the active document (and referenced lines)
// to extract the declaration signature and any preceding comments or docstrings for the symbol.
func ExtractLocalSymbolDocumentation(doc *core.Document, word string, workspaceDir string) (signature string, docLines []string, thumb image.Image, source string) {
	if doc == nil || word == "" {
		return "", nil, nil, ""
	}

	// 1. Check if word is an image path or filename (e.g. "icon.png", "images/logo.jpg")
	cleanWord := strings.Trim(word, `"'`)
	if IsImageFile(cleanWord) {
		candidates := []string{
			cleanWord,
			filepath.Join(filepath.Dir(doc.FilePath), cleanWord),
		}
		if workspaceDir != "" {
			candidates = append(candidates, filepath.Join(workspaceDir, cleanWord))
		}
		for _, cand := range candidates {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				if f, err := os.Open(cand); err == nil {
					img, fmtName, err := image.Decode(f)
					f.Close()
					if err == nil && img != nil {
						dimStr := fmt.Sprintf("%dx%d px", img.Bounds().Dx(), img.Bounds().Dy())
						sig := fmt.Sprintf("Image: %s (%s, %s)", filepath.Base(cand), strings.ToUpper(fmtName), dimStr)
						docs := []string{
							fmt.Sprintf("Path: %s", cand),
							fmt.Sprintf("Size: %s", formatFileSize(fi.Size())),
						}
						return sig, docs, img, "Image Preview"
					}
				}
			}
		}
	}

	// 2. Scan active document buffer for definition patterns
	totLines := doc.Buffer.TotalLines()
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bfunc\s+(?:\([^)]+\)\s+)?` + regexp.QuoteMeta(word) + `\s*\(`),
		regexp.MustCompile(`\btype\s+` + regexp.QuoteMeta(word) + `\b`),
		regexp.MustCompile(`\b(?:var|const)\s+` + regexp.QuoteMeta(word) + `\b`),
		regexp.MustCompile(`\bdef\s+` + regexp.QuoteMeta(word) + `\s*\(`),
		regexp.MustCompile(`\bclass\s+` + regexp.QuoteMeta(word) + `\b`),
		regexp.MustCompile(`\b(?:function|fn)\s+` + regexp.QuoteMeta(word) + `\b`),
		regexp.MustCompile(`\blet\s+` + regexp.QuoteMeta(word) + `\s*=`),
	}

	foundLine := -1
	var foundSig string

	for l := 0; l < totLines; l++ {
		lineBytes, _ := doc.Buffer.GetLine(l)
		lineStr := strings.TrimRight(string(lineBytes), "\r\n")
		for _, pat := range patterns {
			if pat.MatchString(lineStr) {
				foundLine = l
				foundSig = strings.TrimSpace(lineStr)
				// Clean trailing open braces or colons
				foundSig = strings.TrimSuffix(foundSig, " {")
				foundSig = strings.TrimSuffix(foundSig, ":")
				break
			}
		}
		if foundLine >= 0 {
			break
		}
	}

	if foundLine < 0 {
		return "", nil, nil, ""
	}

	// 3. Extract preceding comments immediately above declaration
	var rawComments []string
	inBlockComment := false

	for cl := foundLine - 1; cl >= 0 && cl >= foundLine-15; cl-- {
		b, _ := doc.Buffer.GetLine(cl)
		lStr := strings.TrimSpace(string(b))

		if inBlockComment {
			if strings.Contains(lStr, "/*") {
				idx := strings.Index(lStr, "/*")
				after := strings.TrimSpace(lStr[idx+2:])
				after = strings.TrimPrefix(after, "*")
				after = strings.TrimSpace(after)
				if after != "" {
					rawComments = append([]string{after}, rawComments...)
				}
				inBlockComment = false
			} else {
				lineComment := strings.TrimPrefix(lStr, "*")
				lineComment = strings.TrimSpace(lineComment)
				if lineComment != "" {
					rawComments = append([]string{lineComment}, rawComments...)
				}
			}
			continue
		}

		if lStr == "" {
			break // Empty line breaks doc comment block
		}

		if strings.HasSuffix(lStr, "*/") {
			inBlockComment = true
			before := strings.TrimSuffix(lStr, "*/")
			if strings.HasPrefix(before, "/*") {
				// Single-line block comment: /* ... */
				single := strings.TrimSpace(strings.TrimPrefix(before, "/*"))
				if single != "" {
					rawComments = append([]string{single}, rawComments...)
				}
				inBlockComment = false
			} else {
				lineComment := strings.TrimPrefix(before, "*")
				lineComment = strings.TrimSpace(lineComment)
				if lineComment != "" {
					rawComments = append([]string{lineComment}, rawComments...)
				}
			}
			continue
		}

		if strings.HasPrefix(lStr, "//") {
			comment := strings.TrimSpace(strings.TrimPrefix(lStr, "//"))
			rawComments = append([]string{comment}, rawComments...)
		} else if strings.HasPrefix(lStr, "#") {
			comment := strings.TrimSpace(strings.TrimPrefix(lStr, "#"))
			rawComments = append([]string{comment}, rawComments...)
		} else {
			break
		}
	}

	// If no comments, provide a clean fallback doc line
	if len(rawComments) == 0 {
		rawComments = []string{
			fmt.Sprintf("Declared at line %d in %s", foundLine+1, filepath.Base(doc.FilePath)),
		}
	}

	return foundSig, rawComments, nil, "Code Comments"
}
