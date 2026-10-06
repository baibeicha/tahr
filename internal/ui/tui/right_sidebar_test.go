package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/plugin"
)

func TestRightSidebar_ToggleAndRender(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Initial state: right sidebar closed
	if m.rightSidebarOpen {
		t.Fatalf("expected right sidebar to be closed initially")
	}

	// 2. Toggle AI Chat
	m.ToggleRightSidebar("ai-chat")
	if !m.rightSidebarOpen {
		t.Fatalf("expected right sidebar to be open")
	}
	if m.rightSidebarMode != "ai-chat" {
		t.Fatalf("expected mode ai-chat, got %s", m.rightSidebarMode)
	}

	// Set anim width to target width for synchronous test render
	m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)

	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Right Activity Strip has AI badge at cols (98, 2) and (99, 2)
	cA := frameBuf.Cell(98, 2)
	cI := frameBuf.Cell(99, 2)
	if cA == nil || cA.Rune != 'A' || cI == nil || cI.Rune != 'I' {
		t.Fatalf("expected active badge 'AI' at (98, 2) and (99, 2), got %v %v", cA, cI)
	}

	// Right sidebar divider is at x = 100 - 3 - 34 - 1 = 62
	cDiv := frameBuf.Cell(62, 2)
	if cDiv == nil || cDiv.Rune != '│' {
		t.Fatalf("expected divider '│' at (62, 2), got %v", cDiv)
	}

	// 3. Toggle DB Inspector
	m.ToggleRightSidebar("db-inspector")
	if !m.rightSidebarOpen || m.rightSidebarMode != "db-inspector" {
		t.Fatalf("expected right sidebar to switch to db-inspector")
	}
	m.View(frame)
	cD := frameBuf.Cell(98, 4)
	cB := frameBuf.Cell(99, 4)
	if cD == nil || cD.Rune != 'D' || cB == nil || cB.Rune != 'B' {
		t.Fatalf("expected active badge 'DB' at (98, 4) and (99, 4), got %v %v", cD, cB)
	}

	// 4. Toggle again with same mode -> closes
	m.ToggleRightSidebar("db-inspector")
	if m.rightSidebarOpen {
		t.Fatalf("expected right sidebar to close after second toggle")
	}
	m.rightSidebarAnimWidth = 0
	m.View(frame)
	// Sidebar is closed, divider is no longer at 62
	cBlank := frameBuf.Cell(62, 2)
	if cBlank != nil && cBlank.Rune == '│' {
		t.Fatalf("expected divider to be gone when closed, got %c", cBlank.Rune)
	}
}

func TestToolWindowBadge(t *testing.T) {
	tests := []struct {
		icon   string
		title  string
		wantR1 rune
		wantR2 rune
	}{
		{"[K8s]", "Kubernetes", 'K', '8'},
		{"[Redis]", "Redis", 'R', 'e'},
		{"[SSH]", "Remote SSH", 'S', 'S'},
		{"[DB]", "SQLite", 'D', 'B'},
		{"[Task]", "Tasks", 'T', 'a'},
		{"", "Kafka", 'K', 'a'},
		{"", "X", 'X', ' '},
	}

	for _, tt := range tests {
		r1, r2 := toolWindowBadge(tt.icon, tt.title)
		if r1 != tt.wantR1 || r2 != tt.wantR2 {
			t.Errorf("toolWindowBadge(%q, %q) = (%c, %c), want (%c, %c)", tt.icon, tt.title, r1, r2, tt.wantR1, tt.wantR2)
		}
	}
}

func TestRightSidebar_MouseClicks(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Click on AI badge in Right Activity Strip (x=98, y=2)
	msgAI := tea.MouseMsg{Mouse: input.Mouse{
		X:      98,
		Y:      2,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgAI)
	if !m.rightSidebarOpen || m.rightSidebarMode != "ai-chat" {
		t.Fatalf("expected click on (98, 2) to open AI Assistant, open=%v mode=%s", m.rightSidebarOpen, m.rightSidebarMode)
	}

	// 2. Click on DB badge in Right Activity Strip (x=98, y=4)
	msgDB := tea.MouseMsg{Mouse: input.Mouse{
		X:      98,
		Y:      4,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgDB)
	if !m.rightSidebarOpen || m.rightSidebarMode != "db-inspector" {
		t.Fatalf("expected click on (98, 4) to switch to DB Inspector, open=%v mode=%s", m.rightSidebarOpen, m.rightSidebarMode)
	}

	// 3. Click on GR badge in Right Activity Strip (x=98, y=6)
	msgGR := tea.MouseMsg{Mouse: input.Mouse{
		X:      98,
		Y:      6,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgGR)
	if !m.rightSidebarOpen || m.rightSidebarMode != "project-graphs" {
		t.Fatalf("expected click on (98, 6) to switch to Project Graphs, open=%v mode=%s", m.rightSidebarOpen, m.rightSidebarMode)
	}

	// 4. Click Open Interactive Canvas inside Project Graphs (y=editorTop+5 = 7, x=70)
	msgOpenCanvas := tea.MouseMsg{Mouse: input.Mouse{
		X:      70,
		Y:      7,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgOpenCanvas)
	if m.splits.TotalPanes() < 2 {
		t.Fatalf("expected clicking Open Interactive Canvas to open split pane, totalPanes=%d", m.splits.TotalPanes())
	}

	// 5. Click Close button × in right sidebar header
	// rightStartX = 100 - 3 - 34 = 63
	// close button is at rightStartX + rightSideW - 2 = 63 + 34 - 2 = 95
	msgClose := tea.MouseMsg{Mouse: input.Mouse{
		X:      95,
		Y:      2,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgClose)
	if m.rightSidebarOpen {
		t.Fatalf("expected click on close button to close right sidebar")
	}
}

func TestSplitPane_InteractiveViews(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open DAG Canvas in split
	m.OpenDAGCanvasInSplit()
	if m.splits.TotalPanes() < 2 {
		t.Fatalf("expected at least 2 panes, got %d", m.splits.TotalPanes())
	}
	pane1 := &m.splits.Panes[1]
	if !pane1.IsView() || pane1.ViewID != "dag-canvas" {
		t.Fatalf("expected pane 1 to host dag-canvas view, got isView=%v viewID=%s", pane1.IsView(), pane1.ViewID)
	}

	// Render frame to ensure no panic during canvas layout and channel routing
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Now switch to project-graph
	m.OpenProjectGraphInSplit()
	if pane1.ViewID != "project-graph" {
		t.Fatalf("expected pane 1 to host project-graph view, got %s", pane1.ViewID)
	}
	m.View(frame)

	// Test F3 shortcut opens project-graph in split
	m.splits.SetLayout(SplitSingle)
	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected 1 pane after SetLayout(SplitSingle)")
	}
	_, _ = m.handleKey(input.Key{Type: input.KeyF3, Action: input.KeyPress})
	if m.splits.TotalPanes() < 2 || m.splits.Panes[1].ViewID != "project-graph" {
		t.Fatalf("expected F3 to open project graph in split, total=%d", m.splits.TotalPanes())
	}
}

func TestActivityBars_DynamicPluginDiscoveryAndBadges(t *testing.T) {
	tempDir := t.TempDir()
	pluginsDir := filepath.Join(tempDir, "plugins")
	_ = os.MkdirAll(filepath.Join(pluginsDir, "my-k8s"), 0755)
	_ = os.MkdirAll(filepath.Join(pluginsDir, "my-nb"), 0755)

	manifestK8s := `{
		"id": "my-k8s",
		"name": "Kubernetes Inspector",
		"version": "1.0.0",
		"tool_windows": [
			{
				"id": "k8s-panel",
				"title": "Kubernetes",
				"icon": "[K8s]",
				"position": "right"
			}
		]
	}`
	manifestNB := `{
		"id": "my-nb",
		"name": "Notebooks",
		"version": "1.0.0",
		"tool_windows": [
			{
				"id": "nb-panel",
				"title": "Notebooks",
				"icon": "[NB]",
				"position": "right"
			}
		]
	}`

	_ = os.WriteFile(filepath.Join(pluginsDir, "my-k8s", "plugin.json"), []byte(manifestK8s), 0644)
	_ = os.WriteFile(filepath.Join(pluginsDir, "my-nb", "plugin.json"), []byte(manifestNB), 0644)

	mgr, err := plugin.NewManager(pluginsDir)
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}
	defer mgr.Close()
	mgr.SetProjectDir(tempDir)

	active := mgr.ActiveToolWindows()
	if len(active) != 2 {
		t.Fatalf("expected 2 active tool windows, got %d", len(active))
	}

	// Ensure deterministic ordering (sorted by PluginID then ID)
	if active[0].PluginID != "my-k8s" || active[1].PluginID != "my-nb" {
		t.Fatalf("expected deterministic order [my-k8s, my-nb], got [%s, %s]", active[0].PluginID, active[1].PluginID)
	}

	eng := core.NewEngine()
	m := NewAppModel(eng)
	m.SetPluginManager(mgr)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open omnibar in "tools" mode and verify both plugins are listed
	m.openOmnibar("tools")
	if !m.omnibarOpen || m.omnibarMode != "tools" {
		t.Fatalf("expected omnibar in tools mode")
	}

	foundK8s := false
	foundNB := false
	for _, item := range m.omnibarItems {
		if strings.Contains(item, "Kubernetes") {
			foundK8s = true
		}
		if strings.Contains(item, "Notebooks") {
			foundNB = true
		}
	}
	if !foundK8s || !foundNB {
		t.Fatalf("expected tools omnibar to contain Kubernetes and Notebooks, items: %v", m.omnibarItems)
	}
}

func TestSettings_PluginToggles(t *testing.T) {
	tempDir := t.TempDir()
	pluginsDir := filepath.Join(tempDir, "plugins")
	_ = os.MkdirAll(filepath.Join(pluginsDir, "plugin-a"), 0755)

	manifestA := `{
		"id": "plugin-a",
		"name": "Plugin Alpha",
		"version": "1.0.0",
		"tool_windows": [
			{
				"id": "alpha-panel",
				"title": "Alpha Panel",
				"position": "right"
			}
		]
	}`
	_ = os.WriteFile(filepath.Join(pluginsDir, "plugin-a", "plugin.json"), []byte(manifestA), 0644)

	mgr, err := plugin.NewManager(pluginsDir)
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}
	defer mgr.Close()
	mgr.SetProjectDir(tempDir)

	st := NewSettingsState()
	st.SetPluginManager(mgr)
	st.Open = true
	st.CategoryIdx = 6 // Plugins category
	st.InvalidateFields()

	fields := st.getCategoryFields(6)
	foundToggle := false
	toggleIdx := -1
	for idx, f := range fields {
		if f.Key == "plugin_plugin-a" {
			foundToggle = true
			toggleIdx = idx
			break
		}
	}
	if !foundToggle {
		t.Fatalf("expected to find plugin_plugin-a toggle in Settings Category 6, fields: %+v", fields)
	}

	// Currently enabled
	if !mgr.IsEnabled("plugin-a") {
		t.Fatalf("expected plugin-a to be enabled initially")
	}

	// Select the field and cycle it
	st.FieldIdx = toggleIdx
	st.FocusRight = true
	st.cycleCurrentField()

	// Should now be disabled
	if mgr.IsEnabled("plugin-a") {
		t.Fatalf("expected plugin-a to be disabled after cycleCurrentField")
	}

	// ActiveToolWindows should no longer return alpha-panel
	if len(mgr.ActiveToolWindows()) != 0 {
		t.Fatalf("expected 0 active tool windows after disabling plugin, got %d", len(mgr.ActiveToolWindows()))
	}

	// Cycle again -> re-enabled
	st.cycleCurrentField()
	if !mgr.IsEnabled("plugin-a") {
		t.Fatalf("expected plugin-a to be re-enabled")
	}
	if len(mgr.ActiveToolWindows()) != 1 {
		t.Fatalf("expected 1 active tool window after re-enabling, got %d", len(mgr.ActiveToolWindows()))
	}
}

func TestDatabasePlugins_TabsAndToggle(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Open Database tool window
	m.ToggleRightSidebar("db-inspector")
	if !m.rightSidebarOpen || m.rightSidebarMode != "db-inspector" {
		t.Fatalf("expected right sidebar to be open in db-inspector mode")
	}
	if m.dbSidebarTab != "tables" {
		t.Fatalf("expected initial tab to be 'tables', got %q", m.dbSidebarTab)
	}

	// 2. Click on tab 2: ER-диаграмма
	// rightStartX = 100 - 3 - 34 = 63. contentTop = 4. Tab 2 starts around X=75
	msgTab2 := tea.MouseMsg{Mouse: input.Mouse{
		X:      78,
		Y:      4,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgTab2)
	if m.dbSidebarTab != "er-diagram" {
		t.Fatalf("expected tab to switch to 'er-diagram', got %q", m.dbSidebarTab)
	}
	if m.splits.TotalPanes() < 2 {
		t.Fatalf("expected switching to 'er-diagram' to automatically open split canvas, got %d panes", m.splits.TotalPanes())
	}
	pane1 := m.splits.PaneAt(1)
	if pane1 == nil || !pane1.IsView() || pane1.ViewID != "dag-canvas" {
		t.Fatalf("expected pane 1 to mount dag-canvas, got %v", pane1)
	}

	// 3. Click back to tab 1: Таблицы
	msgTab1 := tea.MouseMsg{Mouse: input.Mouse{
		X:      66,
		Y:      4,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgTab1)
	if m.dbSidebarTab != "tables" {
		t.Fatalf("expected tab to switch back to 'tables', got %q", m.dbSidebarTab)
	}

	// 4. Test Disabling both plugins in PluginManager
	tempDir := t.TempDir()
	pluginsDir := filepath.Join(tempDir, "plugins")
	_ = os.MkdirAll(filepath.Join(pluginsDir, "db-inspector"), 0755)
	_ = os.MkdirAll(filepath.Join(pluginsDir, "db-er-diagram"), 0755)
	_ = os.WriteFile(filepath.Join(pluginsDir, "db-inspector", "plugin.json"), []byte(`{"id":"db-inspector","name":"Database Inspector"}`), 0644)
	_ = os.WriteFile(filepath.Join(pluginsDir, "db-er-diagram", "plugin.json"), []byte(`{"id":"db-er-diagram","name":"Database ER Diagram"}`), 0644)

	pm, err := plugin.NewManager(pluginsDir)
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}
	defer pm.Close()
	m.pluginMgr = pm

	if !pm.IsEnabled("db-inspector") || !pm.IsEnabled("db-er-diagram") {
		t.Fatalf("expected both plugins to be enabled initially")
	}

	// Disable db-inspector -> dbSidebarTab auto-switches to er-diagram
	_ = pm.DisablePlugin("db-inspector")
	m.rightSidebarOpen = false
	m.ToggleRightSidebar("db-inspector")
	if m.dbSidebarTab != "er-diagram" {
		t.Fatalf("expected dbSidebarTab to be 'er-diagram' when db-inspector is disabled, got %q", m.dbSidebarTab)
	}

	// Disable db-er-diagram -> both disabled
	_ = pm.DisablePlugin("db-er-diagram")
	m.rightSidebarOpen = false
	// Clicking DB badge on right strip at (98, 4) should now do nothing because both plugins are disabled
	msgDBCall := tea.MouseMsg{Mouse: input.Mouse{
		X:      98,
		Y:      4,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgDBCall)
	if m.rightSidebarOpen {
		t.Fatalf("expected right sidebar NOT to open when both db plugins are disabled")
	}
}

