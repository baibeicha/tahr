package tui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core"
	"tahr/internal/core/dag"
	"tahr/internal/core/graphs"
	"tahr/internal/core/lsp"
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
	LSPClient    *lsp.Client
	LSPActive    bool
}

// NewProjectGraphPanel initializes a graph panel widget.
func NewProjectGraphPanel(theme *ui.Theme) *ProjectGraphPanel {
	model := dag.NewGraphModel()
	canvas := NewDAGCanvasWidget(model, theme)
	canvas.Kind = "project-graph"
	return &ProjectGraphPanel{
		Mode:        ModeCallHierarchyDownstream,
		Canvas:      canvas,
		FocusRadius: 1,
		Theme:       theme,
	}
}

// SetModel updates the active graph model, calculates Sugiyama layout, and re-routes edges.
func (p *ProjectGraphPanel) SetModel(model *dag.GraphModel) {
	if model == nil {
		model = dag.NewGraphModel()
	}
	if len(model.Nodes) > 0 {
		dag.LayoutSugiyama(model, dag.DefaultSugiyamaConfig())
		router := dag.NewChannelRouter()
		router.RouteAll(model)
	}
	canvas := NewDAGCanvasWidget(model, p.Theme)
	canvas.Kind = "project-graph"
	p.Canvas = canvas
}

// CycleMode advances to the next graph visualization mode.
func (p *ProjectGraphPanel) CycleMode() GraphViewMode {
	p.Mode = (p.Mode + 1) % 4
	return p.Mode
}

// ModeTitle returns a clean label for the current graph mode without brackets.
func (p *ProjectGraphPanel) ModeTitle() string {
	title := ""
	switch p.Mode {
	case ModeCallHierarchyDownstream:
		title = "Call Graph (Downstream)"
	case ModeCallHierarchyUpstream:
		title = "Blast Radius (Upstream)"
	case ModeModuleImports:
		title = "Module Imports & Cycles"
	case ModeRouteToCode:
		title = "Route-to-Code Pipeline"
	default:
		title = "Project Graph"
	}
	if p.LSPActive {
		title += " • LSP"
	}
	return title
}

// RebuildWithLSP analyzes the active document or workspace, immediately presenting a fast universal
// multi-language graph, and asynchronously querying LSP server for type-checked call hierarchy.
func (p *ProjectGraphPanel) RebuildWithLSP(doc *core.Document, workspaceDir string, client *lsp.Client) {
	p.LSPClient = client
	p.LSPActive = (client != nil)

	// 1. Instant universal multi-language graph (0ms turnaround)
	model := BuildProjectGraph(doc, p.Mode, workspaceDir)
	p.SetModel(model)

	// 2. Query richer LSP call hierarchy/symbols in background if language server is active
	// Only query LSP call hierarchy for Downstream and Upstream hierarchy modes.
	if client != nil && doc != nil && doc.FilePath != "" && (p.Mode == ModeCallHierarchyDownstream || p.Mode == ModeCallHierarchyUpstream) {
		cursorLine := 0
		cursorCol := 0
		if sels := doc.Buffer.GetSelections(); len(sels) > 0 {
			cursorLine = sels[0].Head.Line
			cursorCol = sels[0].Head.Column
		}
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		dir := graphs.DirectionDownstream
		if p.Mode == ModeCallHierarchyUpstream {
			dir = graphs.DirectionUpstream
		}

		go func(c *lsp.Client, u string, l, col int, d graphs.HierarchyDirection) {
			node, err := graphs.FetchCallHierarchyFromLSP(c, u, l, col, d, 2)
			// Only update if LSP actually returned relationships/calls. If 0 calls, preserve the full AST graph!
			if err == nil && node != nil && len(node.Children) > 0 {
				lspModel := node.ToGraphModel()
				if len(lspModel.Nodes) > 0 {
					p.SetModel(lspModel)
				}
			}
		}(client, uri, cursorLine, cursorCol, dir)
	}
}

// Rebuild delegates to RebuildWithLSP using existing LSPClient.
func (p *ProjectGraphPanel) Rebuild(doc *core.Document, workspaceDir string) {
	p.RebuildWithLSP(doc, workspaceDir, p.LSPClient)
}

// Render draws the graph panel with top mode bar and interactive DAG canvas.
func (p *ProjectGraphPanel) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	fg := toColor(p.Theme.Foreground)
	activeFg := toColor(p.Theme.Function)
	hdrBg := toColor(p.Theme.SelectionBg)
	errFg := toColor(p.Theme.DiagnosticError)

	// 1. Draw Mini Mode Header Bar (clean text, no brackets or emojis)
	selInfo := ""
	if p.Canvas != nil && p.Canvas.Model != nil && p.Canvas.SelectedNodeID != "" {
		if node, ok := p.Canvas.Model.Nodes[p.Canvas.SelectedNodeID]; ok {
			selInfo = fmt.Sprintf("  •  Выбрано: %s", node.Title)
		}
	}
	headerText := fmt.Sprintf(" Mode: %s%s  •  r: Режим  •  Enter: Перейти ", p.ModeTitle(), selInfo)
	closeText := " ✕ Закрыть (Esc) "
	headerRunes := []rune(headerText)
	closeRunes := []rune(closeText)

	for x := bounds.X; x < bounds.X+bounds.Width; x++ {
		r := ' '
		tFg := fg
		idx := x - bounds.X
		if idx < len(headerRunes) {
			r = headerRunes[idx]
			tFg = activeFg
		}
		buf.SetRune(x, bounds.Y, r, tFg, hdrBg, cell.AttrBold)
	}

	// Close button at right end of header bar
	closeStart := bounds.X + bounds.Width - len(closeRunes)
	if closeStart > bounds.X+len(headerRunes) {
		for i, r := range closeRunes {
			buf.SetRune(closeStart+i, bounds.Y, r, errFg, hdrBg, cell.AttrBold)
		}
	}

	// 2. Render Canvas Widget below header
	if bounds.Height > 1 {
		canvasBounds := buffer.NewRect(bounds.X, bounds.Y+1, bounds.Width, bounds.Height-1)
		p.Canvas.Render(buf, canvasBounds)
	}
}

// BuildProjectGraph constructs a real DAG model from the code document or workspace.
func BuildProjectGraph(doc *core.Document, mode GraphViewMode, workspaceDir string) *dag.GraphModel {
	filePath := ""
	codeContent := ""

	if doc != nil {
		filePath = doc.FilePath
		text, _ := doc.Buffer.GetText()
		codeContent = string(text)
	}

	// Fallback to main.go in workspace if active doc is empty
	if strings.TrimSpace(codeContent) == "" && workspaceDir != "" {
		candidates := []string{
			filepath.Join(workspaceDir, "cmd", "tahr", "main.go"),
			filepath.Join(workspaceDir, "main.go"),
		}
		for _, c := range candidates {
			if data, err := os.ReadFile(c); err == nil && len(data) > 0 {
				filePath = c
				codeContent = string(data)
				break
			}
		}
	}

	model := dag.NewGraphModel()
	if strings.TrimSpace(codeContent) == "" {
		return model
	}

	// 1. Try Go AST analysis first for Go files
	if strings.HasSuffix(filePath, ".go") || strings.Contains(codeContent, "package ") {
		if buildGoGraph(model, filePath, codeContent, mode) {
			return model
		}
	}

	// 2. Generic Regex-based Call & Import Parser for other languages (.rs, .py, .ts, .js, .c)
	buildGenericGraph(model, filePath, codeContent, mode)
	return model
}

func buildGoGraph(model *dag.GraphModel, filePath, code string, mode GraphViewMode) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, code, 0)
	if err != nil || file == nil {
		return false
	}

	fileBase := filepath.Base(filePath)

	switch mode {
	case ModeModuleImports:
		// Module & Package Imports view
		pkgName := "package " + file.Name.Name
		rootCard := &dag.NodeCard{
			ID:    file.Name.Name,
			Title: pkgName,
			Badge: "Package",
			Rows: []dag.CardRow{
				{Name: "File", DataType: fileBase},
				{Name: "Imports", DataType: fmt.Sprintf("%d pkgs", len(file.Imports))},
			},
			Ports: []dag.Port{
				{ID: file.Name.Name + ":out", Side: 'R', Type: dag.PortOutput},
			},
		}
		model.AddNode(rootCard)

		for _, imp := range file.Imports {
			rawPath := strings.Trim(imp.Path.Value, `"`)
			importName := filepath.Base(rawPath)
			badge := "Stdlib"
			if strings.Contains(rawPath, ".") {
				badge = "Dependency"
			} else if strings.Contains(rawPath, "/") {
				badge = "Module"
			}
			impCard := &dag.NodeCard{
				ID:    rawPath,
				Title: importName,
				Badge: badge,
				Rows: []dag.CardRow{
					{Name: "Path", DataType: rawPath},
				},
				Ports: []dag.Port{
					{ID: rawPath + ":in", Side: 'L', Type: dag.PortInput},
				},
			}
			model.AddNode(impCard)
			model.AddEdge(file.Name.Name, file.Name.Name+":out", rawPath, rawPath+":in", dag.MarkerArrow)
		}
		return len(model.Nodes) > 0

	case ModeRouteToCode:
		// Route-to-Code Pipeline: CLI subcommands / HTTP endpoints to handler functions
		foundRoutes := 0
		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok || len(cc.List) == 0 {
					continue
				}
				caseLabel := ""
				for _, expr := range cc.List {
					if lit, ok := expr.(*ast.BasicLit); ok {
						caseLabel = strings.Trim(lit.Value, `"'`)
						break
					} else if ident, ok := expr.(*ast.Ident); ok {
						caseLabel = ident.Name
						break
					}
				}
				if caseLabel == "" {
					continue
				}
				foundRoutes++
				routeID := "route_" + caseLabel
				routeCard := &dag.NodeCard{
					ID:    routeID,
					Title: "Command: " + caseLabel,
					Badge: "Trigger",
					Rows: []dag.CardRow{
						{Name: "Event", DataType: "CLI Dispatch"},
					},
					Ports: []dag.Port{
						{ID: routeID + ":out", Side: 'R', Type: dag.PortOutput},
					},
				}
				model.AddNode(routeCard)

				// Find call target in case body
				for _, s := range cc.Body {
					ast.Inspect(s, func(sn ast.Node) bool {
						call, ok := sn.(*ast.CallExpr)
						if !ok {
							return true
						}
						target := formatCallExpr(call.Fun)
						if target != "" {
							targetID := "fn_" + target
							if _, exists := model.Nodes[targetID]; !exists {
								model.AddNode(&dag.NodeCard{
									ID:    targetID,
									Title: target + "()",
									Badge: "Handler",
									Rows: []dag.CardRow{
										{Name: "Target", DataType: "execution"},
									},
									Ports: []dag.Port{
										{ID: targetID + ":in", Side: 'L', Type: dag.PortInput},
									},
								})
							}
							model.AddEdge(routeID, routeID+":out", targetID, targetID+":in", dag.MarkerArrow)
						}
						return false
					})
				}
			}
			return true
		})
		if foundRoutes > 0 {
			return true
		}
		// Fallthrough to Downstream Call Graph if no explicit switch routes found
		fallthrough

	default:
		// ModeCallHierarchyDownstream & Upstream
		type funcInfo struct {
			name     string
			line     int
			isEntry  bool
			isMethod bool
			calls    []string
		}

		var funcs []funcInfo
		funcMap := make(map[string]bool)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			line := fset.Position(fn.Pos()).Line
			isEntry := (name == "main")
			isMethod := (fn.Recv != nil && len(fn.Recv.List) > 0)

			var calls []string
			seenCalls := make(map[string]bool)
			if fn.Body != nil {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					target := formatCallExpr(call.Fun)
					if target != "" && !seenCalls[target] {
						seenCalls[target] = true
						calls = append(calls, target)
					}
					return true
				})
			}
			funcs = append(funcs, funcInfo{
				name:     name,
				line:     line,
				isEntry:  isEntry,
				isMethod: isMethod,
				calls:    calls,
			})
			funcMap[name] = true
		}

		if len(funcs) == 0 {
			return false
		}

		// Mode 1: Blast Radius (Upstream) -> callers point into targets
		isUpstream := (mode == ModeCallHierarchyUpstream)

		// Create NodeCard for each function declared in this file
		for _, f := range funcs {
			badge := "Function"
			if f.isEntry {
				badge = "Entrypoint"
			} else if f.isMethod {
				badge = "Method"
			}
			card := &dag.NodeCard{
				ID:       f.name,
				Title:    f.name + "()",
				Badge:    badge,
				FilePath: filePath,
				Line:     f.line,
				Rows: []dag.CardRow{
					{Name: "Calls", DataType: fmt.Sprintf("%d targets", len(f.calls))},
					{Name: "Source", DataType: fileBase},
				},
				Ports: []dag.Port{
					{ID: f.name + ":in", Side: 'L', Type: dag.PortInput},
					{ID: f.name + ":out", Side: 'R', Type: dag.PortOutput},
				},
			}
			model.AddNode(card)
		}

		// Add called targets and edges
		targetCount := 0
		for _, f := range funcs {
			for _, target := range f.calls {
				if targetCount >= 18 {
					break
				}
				targetID := target
				if _, exists := model.Nodes[targetID]; !exists {
					badge := "External"
					if funcMap[target] {
						badge = "Local Func"
					} else if strings.Contains(target, ".") {
						badge = "Package"
					}
					targetCard := &dag.NodeCard{
						ID:    targetID,
						Title: target + "()",
						Badge: badge,
						Rows: []dag.CardRow{
							{Name: "Type", DataType: "callee"},
						},
						Ports: []dag.Port{
							{ID: targetID + ":in", Side: 'L', Type: dag.PortInput},
							{ID: targetID + ":out", Side: 'R', Type: dag.PortOutput},
						},
					}
					model.AddNode(targetCard)
					targetCount++
				}

				if isUpstream {
					// Upstream: Target points back to Caller
					model.AddEdge(targetID, targetID+":out", f.name, f.name+":in", dag.MarkerArrow)
				} else {
					// Downstream: Caller points to Callee
					model.AddEdge(f.name, f.name+":out", targetID, targetID+":in", dag.MarkerArrow)
				}
			}
		}
		return len(model.Nodes) > 0
	}
}

func formatCallExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		// Filter out basic builtins that clutter graph
		if e.Name == "len" || e.Name == "append" || e.Name == "make" || e.Name == "panic" {
			return ""
		}
		return e.Name
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			return pkg.Name + "." + e.Sel.Name
		}
		return e.Sel.Name
	default:
		return ""
	}
}

// buildGenericGraph uses universal multi-language extraction for non-Go code (Python, Rust, TS/JS, C/C++, etc.).
func buildGenericGraph(model *dag.GraphModel, filePath, code string, mode GraphViewMode) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch mode {
	case ModeModuleImports:
		depGraph := graphs.NewDependencyGraph()
		buildModuleImportsForGeneric(depGraph, filePath, code, ext)
		depGraph.AnalyzeTarjanSCC()
		m := depGraph.ToGraphModel()
		for _, node := range m.Nodes {
			model.AddNode(node)
		}
		for _, e := range m.Edges {
			model.AddEdge(e.FromNode, e.FromPort, e.ToNode, e.ToPort, e.MarkerEnd)
		}
		return

	case ModeRouteToCode:
		scanner := graphs.NewRoutePipelineScanner()
		scanner.ScanFileContent(filePath, code)
		if len(scanner.Routes) > 0 {
			m := scanner.ToGraphModel()
			for _, node := range m.Nodes {
				model.AddNode(node)
			}
			for _, e := range m.Edges {
				model.AddEdge(e.FromNode, e.FromPort, e.ToNode, e.ToPort, e.MarkerEnd)
			}
			return
		}
		fallthrough

	default:
		buildUniversalCallGraph(model, filePath, code, ext, mode)
	}
}

func buildModuleImportsForGeneric(depGraph *graphs.DependencyGraph, filePath, code, ext string) {
	fileBase := filepath.Base(filePath)
	modName := strings.TrimSuffix(fileBase, filepath.Ext(fileBase))
	if modName == "" {
		modName = "main"
	}

	lines := strings.Split(code, "\n")
	switch ext {
	case ".py":
		reFrom := regexp.MustCompile(`^\s*from\s+([a-zA-Z0-9_.]+)\s+import`)
		reImp := regexp.MustCompile(`^\s*import\s+([a-zA-Z0-9_.]+)`)
		for _, line := range lines {
			if m := reFrom.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			} else if m := reImp.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			}
		}

	case ".ts", ".js", ".tsx", ".jsx":
		reImport := regexp.MustCompile(`(?:import\s+.*?\s+from\s+['"]([^'"]+)['"]|require\(['"]([^'"]+)['"]\))`)
		for _, line := range lines {
			if m := reImport.FindStringSubmatch(line); len(m) > 1 {
				target := m[1]
				if target == "" && len(m) > 2 {
					target = m[2]
				}
				if target != "" {
					depGraph.AddDependency(modName, target)
				}
			}
		}

	case ".rs":
		reUse := regexp.MustCompile(`^\s*(?:pub\s+)?use\s+([a-zA-Z0-9_:]+)`)
		reMod := regexp.MustCompile(`^\s*(?:pub\s+)?mod\s+([a-zA-Z0-9_]+)`)
		for _, line := range lines {
			if m := reUse.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			} else if m := reMod.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			}
		}

	case ".c", ".cpp", ".h", ".hpp":
		reInc := regexp.MustCompile(`^\s*#\s*include\s+[<"]([^>"]+)[>"]`)
		for _, line := range lines {
			if m := reInc.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			}
		}

	default:
		reGeneric := regexp.MustCompile(`(?i)(?:import|require|include|using)\s+['"<]?([a-zA-Z0-9_./\-]+)['">]?`)
		for _, line := range lines {
			if m := reGeneric.FindStringSubmatch(line); len(m) > 1 {
				depGraph.AddDependency(modName, m[1])
			}
		}
	}
}

func buildUniversalCallGraph(model *dag.GraphModel, filePath, code, ext string, mode GraphViewMode) {
	fileBase := filepath.Base(filePath)

	type funcItem struct {
		name     string
		line     int
		bodyText string
	}

	var funcs []funcItem
	lines := strings.Split(code, "\n")

	// Multi-language function declaration patterns
	var reFunc *regexp.Regexp
	switch ext {
	case ".py":
		reFunc = regexp.MustCompile(`^\s*def\s+([a-zA-Z0-9_]+)\s*\(`)
	case ".rs":
		reFunc = regexp.MustCompile(`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([a-zA-Z0-9_]+)\s*\(`)
	case ".ts", ".js", ".tsx", ".jsx":
		reFunc = regexp.MustCompile(`(?:function\s+([a-zA-Z0-9_]+)|const\s+([a-zA-Z0-9_]+)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>)`)
	case ".c", ".cpp", ".h", ".hpp":
		reFunc = regexp.MustCompile(`^\s*(?:[a-zA-Z0-9_*&]+\s+)+([a-zA-Z0-9_]+)\s*\([^;]*\)\s*\{`)
	default:
		reFunc = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?(?:def|fn|function|func)\s+([a-zA-Z0-9_]+)\s*\(`)
	}

	for lineIdx, line := range lines {
		m := reFunc.FindStringSubmatch(line)
		if len(m) > 1 {
			name := m[1]
			if name == "" && len(m) > 2 {
				name = m[2]
			}
			if name != "" && name != "if" && name != "for" && name != "switch" && name != "while" {
				funcs = append(funcs, funcItem{
					name: name,
					line: lineIdx + 1,
				})
			}
		}
	}

	if len(funcs) == 0 {
		return
	}

	// Slice function bodies
	for i := 0; i < len(funcs); i++ {
		startLine := funcs[i].line - 1
		endLine := len(lines)
		if i+1 < len(funcs) {
			endLine = funcs[i+1].line - 1
		}
		if startLine >= 0 && endLine <= len(lines) && startLine < endLine {
			funcs[i].bodyText = strings.Join(lines[startLine:endLine], "\n")
		}
	}

	for _, fn := range funcs {
		badge := "Function"
		if fn.name == "main" || fn.name == "__main__" {
			badge = "Entry"
		}
		card := &dag.NodeCard{
			ID:       fn.name,
			Title:    fn.name + "()",
			Badge:    badge,
			FilePath: filePath,
			Line:     fn.line,
			Rows: []dag.CardRow{
				{Name: fmt.Sprintf("line %d", fn.line), DataType: fileBase},
			},
			Ports: []dag.Port{
				{ID: fn.name + ":in", Side: 'L', Type: dag.PortInput},
				{ID: fn.name + ":out", Side: 'R', Type: dag.PortOutput},
			},
		}
		model.AddNode(card)
	}

	// Connect edges based on callers and callees
	for _, caller := range funcs {
		for _, callee := range funcs {
			if caller.name == callee.name {
				continue
			}
			callPat := regexp.MustCompile(`\b` + regexp.QuoteMeta(callee.name) + `\s*\(`)
			if callPat.MatchString(caller.bodyText) {
				if mode == ModeCallHierarchyUpstream {
					model.AddEdge(callee.name, callee.name+":out", caller.name, caller.name+":in", dag.MarkerArrow)
				} else {
					model.AddEdge(caller.name, caller.name+":out", callee.name, callee.name+":in", dag.MarkerArrow)
				}
			}
		}
	}
}
