package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/dag"
	"tahr/internal/ui"
)

func TestProjectGraphPanel_CycleModeAndRender(t *testing.T) {
	theme := &ui.Theme{
		Foreground:  0xFFFFFF,
		Background:  0x1E1E2E,
		Function:    0x89B4FA,
		SelectionBg: 0x313244,
	}

	panel := NewProjectGraphPanel(theme)
	if panel.Mode != ModeCallHierarchyDownstream {
		t.Fatalf("expected initial mode Downstream, got %v", panel.Mode)
	}

	// Cycle modes
	m1 := panel.CycleMode()
	if m1 != ModeCallHierarchyUpstream || panel.ModeTitle() != "Blast Radius (Upstream)" {
		t.Errorf("expected Upstream mode, got %v (%s)", m1, panel.ModeTitle())
	}
	m2 := panel.CycleMode()
	if m2 != ModeModuleImports {
		t.Errorf("expected ModuleImports, got %v", m2)
	}
	m3 := panel.CycleMode()
	if m3 != ModeRouteToCode {
		t.Errorf("expected RouteToCode, got %v", m3)
	}
	m0 := panel.CycleMode()
	if m0 != ModeCallHierarchyDownstream {
		t.Errorf("expected wrap around to Downstream, got %v", m0)
	}

	// Set model and render
	model := dag.NewGraphModel()
	model.AddNode(&dag.NodeCard{
		ID:    "main",
		Title: "main",
		Rows:  []dag.CardRow{{Name: "main.go:10", DataType: "entry"}},
	})
	panel.SetModel(model)

	buf := buffer.NewBuffer(80, 25)
	panel.Render(buf, buffer.NewRect(0, 0, 80, 25))

	// Verify header rendered without brackets: " Mode: ..."
	cM := buf.Cell(1, 0)
	if cM == nil || cM.Rune != 'M' {
		t.Errorf("expected header rune 'M' at (1, 0), got %v", cM)
	}
}

func TestBuildProjectGraph_GoASTExtraction(t *testing.T) {
	eng := core.NewEngine()
	sampleGo := `package main

import (
	"fmt"
	"os"
)

func printHelp() {
	fmt.Println("Usage: app")
}

func main() {
	if len(os.Args) > 1 {
		printHelp()
		os.Exit(0)
	}
}
`
	doc, err := eng.Open("main.go")
	if err != nil || doc == nil {
		t.Fatalf("failed to open doc: %v", err)
	}
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), sampleGo)

	// 1. Downstream call graph: main -> printHelp, os.Exit, fmt.Println
	graphDown := BuildProjectGraph(doc, ModeCallHierarchyDownstream, "")
	if len(graphDown.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes in call graph, got %d", len(graphDown.Nodes))
	}
	if _, hasMain := graphDown.Nodes["main"]; !hasMain {
		t.Errorf("expected 'main' function node in call graph")
	}
	if _, hasHelp := graphDown.Nodes["printHelp"]; !hasHelp {
		t.Errorf("expected 'printHelp' function node in call graph")
	}
	if len(graphDown.Edges) == 0 {
		t.Errorf("expected directed edges in call graph")
	}

	// 2. Module imports: package main -> fmt, os
	graphImports := BuildProjectGraph(doc, ModeModuleImports, "")
	if len(graphImports.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes in module imports graph, got %d", len(graphImports.Nodes))
	}
	if _, hasFmt := graphImports.Nodes["fmt"]; !hasFmt {
		t.Errorf("expected 'fmt' import node in graph")
	}
	if _, hasOS := graphImports.Nodes["os"]; !hasOS {
		t.Errorf("expected 'os' import node in graph")
	}
}

func TestProjectGraph_CloseSplitMechanisms(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open sample document
	doc, _ := eng.Open("sample.go")
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), "package main\n\nfunc main() {}\n")
	_ = eng.SwitchBuffer(doc.ID)

	// 1. Open project graph in split
	m.OpenProjectGraphInSplit()
	if m.splits.TotalPanes() != 2 {
		t.Fatalf("expected 2 panes after OpenProjectGraphInSplit, got %d", m.splits.TotalPanes())
	}
	if m.splits.ActivePane().ViewID != "project-graph" {
		t.Fatalf("expected active pane to be project-graph, got %s", m.splits.ActivePane().ViewID)
	}

	// 2. Test closing via Esc key
	_, _ = m.handleKey(input.Key{Type: input.KeyEsc})
	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected split pane to close on Esc, total panes: %d", m.splits.TotalPanes())
	}

	// 3. Reopen project graph and test closing via header close button click
	m.OpenProjectGraphInSplit()
	if m.splits.TotalPanes() != 2 {
		t.Fatalf("expected 2 panes after reopen")
	}

	p1 := &m.splits.Panes[1]
	closeClickX := p1.Bounds.X + p1.Bounds.Width - 2
	closeClickY := p1.Bounds.Y

	msgClick := tea.MouseMsg{Mouse: input.Mouse{
		X:      closeClickX,
		Y:      closeClickY,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgClick)
	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected split pane to close on header ✕ click, total panes: %d", m.splits.TotalPanes())
	}

	// 4. Reopen and test closing via Ctrl+W (Rune: 23 is Ctrl+W ASCII)
	m.OpenProjectGraphInSplit()
	if m.splits.TotalPanes() != 2 {
		t.Fatalf("expected 2 panes after reopen")
	}
	_, _ = m.handleKey(input.Key{Rune: 23, BaseKey: 'w'})
	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected split pane to close on Ctrl+W, total panes: %d", m.splits.TotalPanes())
	}
}

func TestBuildProjectGraph_MultiLanguageSupport(t *testing.T) {
	eng := core.NewEngine()

	// 1. Python Call Graph & Module Imports
	pyCode := `from database import connect_db
import os

def process_order(order):
    validate(order)
    connect_db(order)

def validate(order):
    pass
`
	pyDoc, err := eng.Open("service.py")
	if err != nil || pyDoc == nil {
		t.Fatalf("failed to open py doc: %v", err)
	}
	_ = pyDoc.Buffer.ApplyEdit(0, pyDoc.Buffer.TotalBytes(), pyCode)

	pyGraphDown := BuildProjectGraph(pyDoc, ModeCallHierarchyDownstream, "")
	if len(pyGraphDown.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes in Python call graph, got %d", len(pyGraphDown.Nodes))
	}
	if _, hasProcess := pyGraphDown.Nodes["process_order"]; !hasProcess {
		t.Errorf("expected 'process_order' in Python call graph")
	}
	if _, hasValidate := pyGraphDown.Nodes["validate"]; !hasValidate {
		t.Errorf("expected 'validate' in Python call graph")
	}
	if len(pyGraphDown.Edges) == 0 {
		t.Errorf("expected directed edges in Python call graph")
	}

	pyGraphImports := BuildProjectGraph(pyDoc, ModeModuleImports, "")
	if len(pyGraphImports.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes in Python imports graph, got %d", len(pyGraphImports.Nodes))
	}

	// 2. Rust Call Graph & Module Imports
	rsCode := `use std::sync::Arc;
mod auth;

pub fn handle_request(req: Request) {
    authenticate(&req);
    dispatch(req);
}

fn authenticate(req: &Request) -> bool {
    true
}

fn dispatch(req: Request) {
}
`
	rsDoc, err := eng.Open("handler.rs")
	if err != nil || rsDoc == nil {
		t.Fatalf("failed to open rs doc: %v", err)
	}
	_ = rsDoc.Buffer.ApplyEdit(0, rsDoc.Buffer.TotalBytes(), rsCode)

	rsGraphDown := BuildProjectGraph(rsDoc, ModeCallHierarchyDownstream, "")
	if len(rsGraphDown.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes in Rust call graph, got %d", len(rsGraphDown.Nodes))
	}
	if _, hasHandle := rsGraphDown.Nodes["handle_request"]; !hasHandle {
		t.Errorf("expected 'handle_request' in Rust call graph")
	}
	if _, hasAuth := rsGraphDown.Nodes["authenticate"]; !hasAuth {
		t.Errorf("expected 'authenticate' in Rust call graph")
	}
	if _, hasDispatch := rsGraphDown.Nodes["dispatch"]; !hasDispatch {
		t.Errorf("expected 'dispatch' in Rust call graph")
	}
	if len(rsGraphDown.Edges) < 2 {
		t.Errorf("expected at least 2 edges in Rust call graph, got %d", len(rsGraphDown.Edges))
	}

	// 3. TypeScript / JavaScript Call Graph
	tsCode := `import { db } from './db';

function handleLogin(user: User) {
    verifyPassword(user);
    createSession(user);
}

function verifyPassword(user: User) {
}

function createSession(user: User) {
}
`
	tsDoc, err := eng.Open("auth.ts")
	if err != nil || tsDoc == nil {
		t.Fatalf("failed to open ts doc: %v", err)
	}
	_ = tsDoc.Buffer.ApplyEdit(0, tsDoc.Buffer.TotalBytes(), tsCode)

	tsGraphDown := BuildProjectGraph(tsDoc, ModeCallHierarchyDownstream, "")
	if len(tsGraphDown.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes in TypeScript call graph, got %d", len(tsGraphDown.Nodes))
	}
	if _, hasLogin := tsGraphDown.Nodes["handleLogin"]; !hasLogin {
		t.Errorf("expected 'handleLogin' in TypeScript call graph")
	}
	if _, hasVerify := tsGraphDown.Nodes["verifyPassword"]; !hasVerify {
		t.Errorf("expected 'verifyPassword' in TypeScript call graph")
	}
}

func TestProjectGraph_LSPIntegration(t *testing.T) {
	theme := &ui.Theme{
		Foreground:  0xFFFFFF,
		Background:  0x1E1E2E,
		Function:    0x89B4FA,
		SelectionBg: 0x313244,
	}
	panel := NewProjectGraphPanel(theme)

	eng := core.NewEngine()
	doc, _ := eng.Open("main.py")
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), "def run():\n    print('hello')\n")

	// Test RebuildWithLSP without crashing when client is nil
	panel.RebuildWithLSP(doc, "", nil)
	if len(panel.Canvas.Model.Nodes) == 0 {
		t.Errorf("expected fast local graph fallback when LSP client is nil")
	}

	if panel.LSPActive {
		t.Errorf("expected LSPActive to be false when client is nil")
	}

	// Verify ModeTitle reflects clean title
	if panel.ModeTitle() != "Call Graph (Downstream)" {
		t.Errorf("unexpected ModeTitle: %s", panel.ModeTitle())
	}
}

func TestProjectGraph_EditorTab(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open sample document
	doc, _ := eng.Open("service.go")
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), "package main\n\nfunc Run() {}\nfunc Init() { Run() }\n")

	// 1. Open project graph via editor tab
	m.OpenProjectGraphTab()

	activeDoc := m.eng.ActiveDocument()
	if activeDoc == nil || filepath.Base(activeDoc.FilePath) != "project.graph" {
		t.Fatalf("expected active document to be project.graph, got %v", activeDoc)
	}

	// Must be in a single pane, NOT in a split
	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected 1 pane (editor tab), got %d panes", m.splits.TotalPanes())
	}

	// Model must have parsed the functions (Run and Init)
	if m.projectGraphPanel == nil || len(m.projectGraphPanel.Canvas.Model.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes from service.go in project graph, got %v", m.projectGraphPanel.Canvas.Model.Nodes)
	}

	// 2. Press Esc to close editor tab
	_, _ = m.handleKey(input.Key{Type: input.KeyEsc})
	newActive := m.eng.ActiveDocument()
	if newActive != nil && filepath.Base(newActive.FilePath) == "project.graph" {
		t.Fatalf("expected project.graph to be closed after Esc")
	}
}

func TestProjectGraph_MouseNavigation(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	doc, _ := eng.Open("main.go")
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), "package main\n\nfunc main() {}\n")

	m.OpenProjectGraphTab()
	if m.projectGraphPanel == nil || m.projectGraphPanel.Canvas == nil {
		t.Fatalf("expected projectGraphPanel and canvas to be initialized")
	}

	origPanX := m.projectGraphPanel.Canvas.PanX
	origPanY := m.projectGraphPanel.Canvas.PanY

	// 1. MousePress inside editor canvas
	_, _ = m.handleMouse(tea.MouseMsg{Mouse: input.Mouse{
		X:      30,
		Y:      10,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}})
	if !m.canvasDragging {
		t.Errorf("expected canvasDragging to be true after MousePress")
	}

	// 2. MouseDrag by (+5, +3)
	_, _ = m.handleMouse(tea.MouseMsg{Mouse: input.Mouse{
		X:      35,
		Y:      13,
		Button: input.MouseLeft,
		Action: input.MouseDrag,
	}})
	if m.projectGraphPanel.Canvas.PanX != origPanX+5 {
		t.Errorf("expected PanX to increase by 5, got %d (orig: %d)", m.projectGraphPanel.Canvas.PanX, origPanX)
	}
	if m.projectGraphPanel.Canvas.PanY != origPanY+3 {
		t.Errorf("expected PanY to increase by 3, got %d (orig: %d)", m.projectGraphPanel.Canvas.PanY, origPanY)
	}

	// 3. MouseRelease
	_, _ = m.handleMouse(tea.MouseMsg{Mouse: input.Mouse{
		Button: input.MouseLeft,
		Action: input.MouseRelease,
	}})
	if m.canvasDragging {
		t.Errorf("expected canvasDragging to be false after MouseRelease")
	}

	// 4. MouseWheel scrolling
	beforeWheelY := m.projectGraphPanel.Canvas.PanY
	_, _ = m.handleMouse(tea.MouseMsg{Mouse: input.Mouse{
		Button: input.MouseWheelUp,
	}})
	if m.projectGraphPanel.Canvas.PanY <= beforeWheelY {
		t.Errorf("expected PanY to increase on MouseWheelUp, before: %d, after: %d", beforeWheelY, m.projectGraphPanel.Canvas.PanY)
	}

	// 5. Horizontal wheel scrolling
	beforeWheelX := m.projectGraphPanel.Canvas.PanX
	_, _ = m.handleMouse(tea.MouseMsg{Mouse: input.Mouse{
		Button: input.MouseWheelLeft,
	}})
	if m.projectGraphPanel.Canvas.PanX <= beforeWheelX {
		t.Errorf("expected PanX to increase on MouseWheelLeft, before: %d, after: %d", beforeWheelX, m.projectGraphPanel.Canvas.PanX)
	}
}

func TestProjectGraph_HeaderNoGarbledUTF8(t *testing.T) {
	theme := &ui.Theme{
		Foreground:  0xFFFFFF,
		Background:  0x1E1E2E,
		Function:    0x89B4FA,
		SelectionBg: 0x313244,
	}
	panel := NewProjectGraphPanel(theme)

	buf := buffer.NewBuffer(100, 10)
	panel.Render(buf, buffer.NewRect(0, 0, 100, 10))

	// Verify row 0 contains no broken UTF-8 byte artefacts like 'â'
	rowRunes := make([]rune, 100)
	for x := 0; x < 100; x++ {
		c := buf.Cell(x, 0)
		if c != nil {
			rowRunes[x] = c.Rune
			if c.Rune == 'â' {
				t.Fatalf("detected garbled UTF-8 byte artifact 'â' at x=%d in header row: %s", x, string(rowRunes[:x+1]))
			}
		}
	}

	headerStr := string(rowRunes)
	if !strings.Contains(headerStr, "•") {
		t.Errorf("expected clean bullet '•' in header row, got %s", headerStr)
	}
	if !strings.Contains(headerStr, "Режим") {
		t.Errorf("expected Russian word 'Режим' in header row, got %s", headerStr)
	}
}

