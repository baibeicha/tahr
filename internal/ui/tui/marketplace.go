package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// MarketplaceModal provides a fullscreen dual-pane extension browser and repository manager.
type MarketplaceModal struct {
	Open       bool
	mgr        *plugin.Manager
	ActiveTab  int // 0: Browse, 1: Installed, 2: Repositories
	Categories []string
	CatIdx     int // Category index

	// Search & Navigation
	SearchQuery   string
	SearchFocused bool
	FocusRight    bool

	// Plugin list
	Plugins       []plugin.RemotePluginInfo
	SelectedIdx   int
	ScrollOffset  int

	// Repositories Management
	Repositories  []plugin.PluginRepository
	SelectedRepo  int
	AddingRepo    bool
	EditingRepoID string
	AddRepoField  int // 0: Name, 1: URL, 2: Type
	AddRepoName   string
	AddRepoURL    string
	AddRepoType   string
	RepoTestMsg   string

	StatusMessage string
	StatusIsError bool
}

func determinePluginCategory(inst *plugin.Manifest) string {
	if inst == nil {
		return "tools"
	}
	if inst.Category != "" {
		return strings.ToLower(inst.Category)
	}
	if len(inst.Localizations) > 0 {
		return "i18n"
	}
	if len(inst.Themes) > 0 {
		return "theme"
	}
	if inst.LSP != nil {
		return "lsp"
	}
	if inst.DAP != nil {
		return "dap"
	}
	return "tools"
}

// NewMarketplaceModal creates a new marketplace modal instance.
func NewMarketplaceModal(mgr *plugin.Manager) *MarketplaceModal {
	m := &MarketplaceModal{
		Open:          false,
		mgr:           mgr,
		ActiveTab:     0,
		Categories:    []string{"All", "LSP", "DAP", "Theme", "Tools", "i18n", "Database", "Infrastructure"},
		CatIdx:        0,
		SearchQuery:   "",
		SearchFocused: false,
		FocusRight:    false,
		Plugins:       make([]plugin.RemotePluginInfo, 0),
		Repositories:  make([]plugin.PluginRepository, 0),
		AddRepoType:   "http",
	}
	m.Refresh()
	return m
}

// Refresh reloads plugins and repository data from the plugin manager.
func (m *MarketplaceModal) Refresh() {
	if m.mgr == nil {
		m.Plugins = nil
		m.Repositories = plugin.DefaultRepositories()
		return
	}

	m.Repositories = m.mgr.GetRepositories()
	cat := ""
	if m.CatIdx > 0 && m.CatIdx < len(m.Categories) {
		cat = strings.ToLower(m.Categories[m.CatIdx])
	}

	if m.ActiveTab == 1 { // Installed tab
		var installedOnly []plugin.RemotePluginInfo
		for _, inst := range m.mgr.InstalledPlugins() {
			pCat := determinePluginCategory(inst)
			catMatch := (cat == "" || cat == "all" || strings.EqualFold(pCat, cat))
			if !catMatch {
				if cat == "dap" && inst.DAP != nil {
					catMatch = true
				} else if cat == "lsp" && inst.LSP != nil {
					catMatch = true
				} else if cat == "theme" && len(inst.Themes) > 0 {
					catMatch = true
				} else if cat == "i18n" && len(inst.Localizations) > 0 {
					catMatch = true
				}
			}
			if !catMatch {
				for _, l := range inst.Languages {
					if strings.EqualFold(l.ID, cat) {
						catMatch = true
						break
					}
				}
			}
			if !catMatch {
				continue
			}

			q := strings.ToLower(m.SearchQuery)
			if q != "" {
				if !strings.Contains(strings.ToLower(inst.Name), q) &&
					!strings.Contains(strings.ToLower(inst.ID), q) &&
					!strings.Contains(strings.ToLower(inst.Description), q) {
					continue
				}
			}

			installedOnly = append(installedOnly, plugin.RemotePluginInfo{
				ID:          inst.ID,
				Name:        inst.Name,
				Version:     inst.Version,
				Author:      inst.Author,
				Description: inst.Description,
				Category:    pCat,
				Installed:   true,
				Enabled:     m.mgr.IsEnabled(inst.ID),
			})
		}
		m.Plugins = installedOnly
	} else {
		plugins, err := m.mgr.FetchCatalog(m.SearchQuery, cat)
		if err == nil {
			for i := range plugins {
				plugins[i].Enabled = m.mgr.IsEnabled(plugins[i].ID)
			}
			m.Plugins = plugins
		} else {
			m.Plugins = nil
		}
	}

	if m.SelectedIdx >= len(m.Plugins) {
		m.SelectedIdx = max(0, len(m.Plugins)-1)
	}
	if m.SelectedRepo >= len(m.Repositories) {
		m.SelectedRepo = max(0, len(m.Repositories)-1)
	}
}

// HandleKey processes keyboard navigation in the marketplace window.
func (m *MarketplaceModal) HandleKey(k input.Key) (handled bool, shouldClose bool) {
	if !m.Open {
		return false, false
	}

	// 1. Adding Repository Form Input Mode
	if m.AddingRepo {
		switch k.Type {
		case input.KeyEsc:
			m.AddingRepo = false
			m.EditingRepoID = ""
			return true, false

		case input.KeyTab:
			m.AddRepoField = (m.AddRepoField + 1) % 3
			return true, false

		case input.KeyBacktab:
			m.AddRepoField = (m.AddRepoField + 2) % 3
			return true, false

		case input.KeyEnter:
			if m.AddRepoURL != "" {
				name := m.AddRepoName
				if name == "" {
					name = m.AddRepoURL
				}
				repoType := m.AddRepoType
				if repoType == "" {
					repoType = "http"
				}

				if m.EditingRepoID != "" {
					updatedRepo := plugin.PluginRepository{
						ID:      m.EditingRepoID,
						Name:    name,
						URL:     m.AddRepoURL,
						Type:    repoType,
						Enabled: true,
					}
					if m.mgr != nil {
						_ = m.mgr.UpdateRepository(updatedRepo)
					}
					m.StatusMessage = fmt.Sprintf("Updated repository '%s'", name)
				} else {
					newRepo := plugin.PluginRepository{
						Name:    name,
						URL:     m.AddRepoURL,
						Type:    repoType,
						Enabled: true,
					}
					if m.mgr != nil {
						_ = m.mgr.AddRepository(newRepo)
					}
					m.StatusMessage = fmt.Sprintf("Added repository '%s'", name)
				}

				m.StatusIsError = false
				m.AddingRepo = false
				m.EditingRepoID = ""
				m.AddRepoName = ""
				m.AddRepoURL = ""
				m.Refresh()
			}
			return true, false

		case input.KeyBackspace:
			switch m.AddRepoField {
			case 0:
				if len(m.AddRepoName) > 0 {
					r := []rune(m.AddRepoName)
					m.AddRepoName = string(r[:len(r)-1])
				}
			case 1:
				if len(m.AddRepoURL) > 0 {
					r := []rune(m.AddRepoURL)
					m.AddRepoURL = string(r[:len(r)-1])
				}
			case 2:
				if len(m.AddRepoType) > 0 {
					r := []rune(m.AddRepoType)
					m.AddRepoType = string(r[:len(r)-1])
				}
			}
			return true, false

		default:
			if k.Rune >= 32 {
				switch m.AddRepoField {
				case 0:
					m.AddRepoName += string(k.Rune)
				case 1:
					m.AddRepoURL += string(k.Rune)
				case 2:
					m.AddRepoType += string(k.Rune)
				}
				return true, false
			}
		}
		return true, false
	}

	// 2. Search Field Typing Mode
	if m.SearchFocused {
		switch k.Type {
		case input.KeyEsc:
			m.SearchFocused = false
			return true, false

		case input.KeyEnter, input.KeyDown, input.KeyTab:
			m.SearchFocused = false
			return true, false

		case input.KeyBackspace:
			if len(m.SearchQuery) > 0 {
				r := []rune(m.SearchQuery)
				m.SearchQuery = string(r[:len(r)-1])
				m.Refresh()
			}
			return true, false

		default:
			if k.Rune >= 32 {
				m.SearchQuery += string(k.Rune)
				m.Refresh()
				return true, false
			}
		}
		return true, false
	}

	// 3. Global Modal Navigation
	switch k.Type {
	case input.KeyEsc:
		m.Open = false
		return true, true

	case input.KeyTab:
		m.FocusRight = !m.FocusRight
		return true, false

	case input.KeyUp:
		if m.ActiveTab == 2 { // Repositories
			if m.SelectedRepo > 0 {
				m.SelectedRepo--
			}
		} else {
			if m.SelectedIdx > 0 {
				m.SelectedIdx--
				if m.SelectedIdx < m.ScrollOffset {
					m.ScrollOffset = m.SelectedIdx
				}
			}
		}
		return true, false

	case input.KeyDown:
		if m.ActiveTab == 2 { // Repositories
			if m.SelectedRepo+1 < len(m.Repositories) {
				m.SelectedRepo++
			}
		} else {
			if m.SelectedIdx+1 < len(m.Plugins) {
				m.SelectedIdx++
				if m.SelectedIdx >= m.ScrollOffset+10 {
					m.ScrollOffset = m.SelectedIdx - 9
				}
			}
		}
		return true, false

	case input.KeyLeft:
		if m.CatIdx > 0 {
			m.CatIdx--
			m.Refresh()
		}
		return true, false

	case input.KeyRight:
		if m.CatIdx+1 < len(m.Categories) {
			m.CatIdx++
			m.Refresh()
		}
		return true, false

	case input.KeyEnter:
		if m.ActiveTab == 2 {
			// Test selected repository
			if m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
				repo := m.Repositories[m.SelectedRepo]
				if m.mgr != nil {
					ok, msg, _ := m.mgr.TestRepository(repo)
					m.RepoTestMsg = msg
					m.StatusIsError = !ok
				}
			}
			return true, false
		}

		if m.ActiveTab == 1 {
			// Installed tab: toggle enabled/disabled
			if m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
				p := m.Plugins[m.SelectedIdx]
				if m.mgr != nil {
					if m.mgr.IsEnabled(p.ID) {
						_ = m.mgr.DisablePlugin(p.ID)
						m.StatusMessage = fmt.Sprintf("Disabled '%s'", p.Name)
					} else {
						_ = m.mgr.EnablePlugin(p.ID)
						m.StatusMessage = fmt.Sprintf("Enabled '%s'", p.Name)
					}
					m.StatusIsError = false
					m.Refresh()
				}
			}
			return true, false
		}

		// Tab 0: Install selected plugin
		if m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
			p := m.Plugins[m.SelectedIdx]
			if p.Installed {
				m.StatusMessage = fmt.Sprintf("Plugin '%s' is already installed", p.Name)
			} else {
				if m.mgr != nil {
					targetURL := p.DownloadURL
					if targetURL == "" {
						targetURL = p.ID
					}
					_, err := m.mgr.InstallFromURL(targetURL)
					if err != nil {
						m.StatusMessage = fmt.Sprintf("Install failed: %v", err)
						m.StatusIsError = true
					} else {
						m.StatusMessage = fmt.Sprintf("Installed '%s'", p.Name)
						m.StatusIsError = false
						m.Refresh()
					}
				}
			}
		}
		return true, false

	case input.KeySpace:
		if m.ActiveTab == 1 && m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
			p := m.Plugins[m.SelectedIdx]
			if m.mgr != nil {
				if m.mgr.IsEnabled(p.ID) {
					_ = m.mgr.DisablePlugin(p.ID)
					m.StatusMessage = fmt.Sprintf("Disabled '%s'", p.Name)
				} else {
					_ = m.mgr.EnablePlugin(p.ID)
					m.StatusMessage = fmt.Sprintf("Enabled '%s'", p.Name)
				}
				m.StatusIsError = false
				m.Refresh()
			}
			return true, false
		}
		if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
			repo := m.Repositories[m.SelectedRepo]
			if m.mgr != nil {
				_ = m.mgr.ToggleRepository(repo.ID)
				m.Refresh()
			}
			return true, false
		}

	case input.KeyDelete:
		if (m.ActiveTab == 0 || m.ActiveTab == 1) && m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
			p := m.Plugins[m.SelectedIdx]
			if m.mgr != nil && p.Installed {
				_ = m.mgr.Uninstall(p.ID)
				m.StatusMessage = fmt.Sprintf("Uninstalled '%s'", p.Name)
				m.StatusIsError = false
				m.Refresh()
			}
			return true, false
		}
		if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
			repo := m.Repositories[m.SelectedRepo]
			if m.mgr != nil {
				_ = m.mgr.RemoveRepository(repo.ID)
				m.StatusMessage = fmt.Sprintf("Removed repository '%s'", repo.Name)
				m.StatusIsError = false
				m.Refresh()
			}
			return true, false
		}

	case input.KeyRune:
		switch k.Rune {
		case '1':
			m.ActiveTab = 0
			m.Refresh()
			return true, false
		case '2':
			m.ActiveTab = 1
			m.Refresh()
			return true, false
		case '3':
			m.ActiveTab = 2
			m.Refresh()
			return true, false

		case '/', 'f', 'F':
			m.SearchFocused = true
			return true, false

		case 'r', 'R':
			m.Refresh()
			m.StatusMessage = "Marketplace reloaded"
			m.StatusIsError = false
			return true, false

		case 'a', 'A':
			if m.ActiveTab == 2 {
				m.AddingRepo = true
				m.EditingRepoID = ""
				m.AddRepoField = 0
				m.AddRepoName = ""
				m.AddRepoURL = ""
				m.AddRepoType = "http"
				return true, false
			}

		case 'e', 'E':
			if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
				repo := m.Repositories[m.SelectedRepo]
				m.AddingRepo = true
				m.EditingRepoID = repo.ID
				m.AddRepoName = repo.Name
				m.AddRepoURL = repo.URL
				m.AddRepoType = repo.Type
				m.AddRepoField = 1 // Start on URL field for rapid editing
				return true, false
			}

		case 't', 'T':
			if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
				repo := m.Repositories[m.SelectedRepo]
				if m.mgr != nil {
					ok, msg, _ := m.mgr.TestRepository(repo)
					m.RepoTestMsg = msg
					m.StatusIsError = !ok
				}
				return true, false
			}

		case ' ':
			if m.ActiveTab == 1 && m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
				p := m.Plugins[m.SelectedIdx]
				if m.mgr != nil {
					if m.mgr.IsEnabled(p.ID) {
						_ = m.mgr.DisablePlugin(p.ID)
						m.StatusMessage = fmt.Sprintf("Disabled '%s'", p.Name)
					} else {
						_ = m.mgr.EnablePlugin(p.ID)
						m.StatusMessage = fmt.Sprintf("Enabled '%s'", p.Name)
					}
					m.StatusIsError = false
					m.Refresh()
				}
				return true, false
			}
			if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
				repo := m.Repositories[m.SelectedRepo]
				if m.mgr != nil {
					_ = m.mgr.ToggleRepository(repo.ID)
					m.Refresh()
				}
				return true, false
			}

		case 'u', 'U':
			// Uninstall plugin
			if (m.ActiveTab == 0 || m.ActiveTab == 1) && m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
				p := m.Plugins[m.SelectedIdx]
				if m.mgr != nil && p.Installed {
					_ = m.mgr.Uninstall(p.ID)
					m.StatusMessage = fmt.Sprintf("Uninstalled '%s'", p.Name)
					m.StatusIsError = false
					m.Refresh()
				}
			}
			return true, false

		case 'd', 'D':
			if (m.ActiveTab == 0 || m.ActiveTab == 1) && m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
				p := m.Plugins[m.SelectedIdx]
				if m.mgr != nil && p.Installed {
					_ = m.mgr.Uninstall(p.ID)
					m.StatusMessage = fmt.Sprintf("Uninstalled '%s'", p.Name)
					m.StatusIsError = false
					m.Refresh()
				}
				return true, false
			}
			if m.ActiveTab == 2 && m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
				repo := m.Repositories[m.SelectedRepo]
				if m.mgr != nil {
					_ = m.mgr.RemoveRepository(repo.ID)
					m.StatusMessage = fmt.Sprintf("Removed repository '%s'", repo.Name)
					m.StatusIsError = false
					m.Refresh()
				}
			}
			return true, false
		}
	}

	return false, false
}

// Render draws the fullscreen Marketplace UI.
func (m *MarketplaceModal) Render(buf *buffer.Buffer, w, h int, th ui.Theme) {
	if !m.Open || buf == nil || w < 40 || h < 14 {
		return
	}

	modalW := w - 4
	modalH := h - 2
	if modalW < 40 {
		modalW = w
	}
	if modalH < 14 {
		modalH = h
	}

	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	themeBg := toColor(th.Background)
	themeFg := toColor(th.Foreground)
	borderFg := toColor(th.BorderColor)
	accentFg := toColor(th.Function)
	keywordFg := toColor(th.Keyword)
	stringFg := toColor(th.String)
	commentFg := toColor(th.Comment)
	selBg := toColor(th.SelectionBg)
	selFg := toColor(th.Foreground)

	// 1. Draw outer rounded box & backdrop fill
	for y := 0; y < modalH; y++ {
		for x := 0; x < modalW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == modalW-1 {
				ch = '╮'
			} else if y == modalH-1 && x == 0 {
				ch = '╰'
			} else if y == modalH-1 && x == modalW-1 {
				ch = '╯'
			} else if y == 0 || y == modalH-1 {
				ch = '─'
			} else if x == 0 || x == modalW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, themeBg, cell.AttrNone)
		}
	}

	// Title
	title := i18n.T("market.title")
	drawString(buf, startX+2, startY, title, accentFg, themeBg, cell.AttrBold, modalW-4)

	// 2. Tabs Row (Row startY+1)
	tabs := []string{i18n.T("market.tab_browse"), i18n.T("market.tab_installed"), i18n.T("market.tab_repos")}
	tabX := startX + 2
	for idx, tab := range tabs {
		tBg := themeBg
		tFg := commentFg
		attr := cell.AttrNone
		if idx == m.ActiveTab {
			tFg = accentFg
			attr = cell.AttrBold
		}
		for _, r := range []rune(tab) {
			buf.SetRune(tabX, startY+1, r, tFg, tBg, attr)
			tabX++
		}
		tabX += 2
	}

	// Tab separator line
	for x := 1; x < modalW-1; x++ {
		buf.SetRune(startX+x, startY+2, '─', borderFg, themeBg, cell.AttrNone)
	}

	// 3. Search & Category bar (Row startY+3)
	if m.ActiveTab != 2 {
		searchLabel := i18n.T("market.search")
		drawString(buf, startX+2, startY+3, searchLabel, themeFg, themeBg, cell.AttrNone, modalW-4)
		sqX := startX + 2 + len([]rune(searchLabel))
		sqBg := themeBg
		sqFg := themeFg
		if m.SearchFocused {
			sqBg = selBg
			sqFg = selFg
		}
		queryBox := fmt.Sprintf(" %s_ ", m.SearchQuery)
		for i, r := range []rune(queryBox) {
			buf.SetRune(sqX+i, startY+3, r, sqFg, sqBg, cell.AttrBold)
		}

		catDisplay := i18n.T("market.cat." + strings.ToLower(m.Categories[m.CatIdx]))
		catLabel := fmt.Sprintf("%s: ◄ %s ►", i18n.T("market.category"), catDisplay)
		catRunes := []rune(catLabel)
		catX := startX + modalW - len(catRunes) - 3
		for i, r := range catRunes {
			buf.SetRune(catX+i, startY+3, r, keywordFg, themeBg, cell.AttrNone)
		}

		// Underline
		for x := 1; x < modalW-1; x++ {
			buf.SetRune(startX+x, startY+4, '─', borderFg, themeBg, cell.AttrNone)
		}
	}

	// 4. Dual Pane Layout
	bodyTop := startY + 5
	if m.ActiveTab == 2 {
		bodyTop = startY + 3
	}
	bodyH := modalH - (bodyTop - startY) - 2

	leftW := modalW * 42 / 100
	if leftW < 30 {
		leftW = 30
	}
	divX := startX + leftW

	for y := bodyTop; y < bodyTop+bodyH; y++ {
		buf.SetRune(divX, y, '│', borderFg, themeBg, cell.AttrNone)
	}

	// -------------------------------------------------------------
	// TAB 0 & 1: Plugins Dual Pane
	// -------------------------------------------------------------
	if m.ActiveTab == 0 || m.ActiveTab == 1 {
		// Left: Plugin list
		visibleRows := bodyH / 2
		for i := 0; i < visibleRows; i++ {
			pIdx := m.ScrollOffset + i
			if pIdx >= len(m.Plugins) {
				break
			}
			p := m.Plugins[pIdx]
			rowY := bodyTop + i*2

			isCur := (pIdx == m.SelectedIdx)
			pBg := themeBg
			pFg := themeFg
			attr := cell.AttrNone
			if isCur {
				pBg = selBg
				pFg = selFg
				attr = cell.AttrBold
			}

			// Line 1: Name, Version, Category badge
			catBadge := fmt.Sprintf("[%s]", strings.ToUpper(p.Category))
			leftStr := fmt.Sprintf("%s %s", p.Name, p.Version)
			leftRunes := []rune(leftStr)
			badgeRunes := []rune(catBadge)
			maxLen := leftW - len(badgeRunes) - 5
			if maxLen > 0 && len(leftRunes) > maxLen {
				leftStr = string(leftRunes[:maxLen]) + ".."
			}
			topLine := fmt.Sprintf(" %s", leftStr)
			for col := 0; col < leftW-2; col++ {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY, ' ', pFg, pBg, attr)
				}
			}
			for col, r := range []rune(topLine) {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY, r, pFg, pBg, attr)
				}
			}
			badgeX := startX + leftW - len(badgeRunes) - 2
			for col, r := range badgeRunes {
				if badgeX+col < divX {
					buf.SetRune(badgeX+col, rowY, r, keywordFg, pBg, attr)
				}
			}

			// Line 2: Author, Repository, Installed status
			statusStr := ""
			if p.Installed {
				if p.Enabled {
					statusStr = "[✓] Enabled"
				} else {
					statusStr = "[ ] Disabled"
				}
			}
			subLine := fmt.Sprintf("   by %s (%s) %s", p.Author, p.RepoName, statusStr)
			for col := 0; col < leftW-2; col++ {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY+1, ' ', themeFg, pBg, cell.AttrNone)
				}
			}
			sFg := commentFg
			if strings.Contains(statusStr, "[✓]") {
				sFg = stringFg
			}
			if isCur {
				sFg = pFg
			}
			for col, r := range []rune(subLine) {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY+1, r, sFg, pBg, cell.AttrNone)
				}
			}
		}

		// Right: Plugin Details Pane
		rightStartX := divX + 2
		rightW := modalW - leftW - 4

		if m.SelectedIdx >= 0 && m.SelectedIdx < len(m.Plugins) {
			p := m.Plugins[m.SelectedIdx]

			// Header: Title & Version
			hStr := fmt.Sprintf("%s  v%s", p.Name, p.Version)
			drawString(buf, rightStartX, bodyTop, hStr, accentFg, themeBg, cell.AttrBold, rightW)

			// Author & Source
			metaStr := i18n.T("market.author", p.Author, p.RepoName)
			drawString(buf, rightStartX, bodyTop+1, metaStr, commentFg, themeBg, cell.AttrNone, rightW)

			// Action buttons row
			actionRow := i18n.T("market.install")
			btnFg := stringFg
			if p.Installed {
				if p.Enabled {
					actionRow = i18n.T("market.enabled")
				} else {
					actionRow = i18n.T("market.disabled")
					btnFg = commentFg
				}
			}
			drawString(buf, rightStartX, bodyTop+3, actionRow, btnFg, themeBg, cell.AttrBold, rightW)

			// Description
			buf.SetRune(rightStartX, bodyTop+5, '─', borderFg, themeBg, cell.AttrNone)
			descLines := wrapString(p.Description, rightW)
			for idx, dl := range descLines {
				if bodyTop+6+idx < startY+modalH-2 {
					drawString(buf, rightStartX, bodyTop+6+idx, dl, themeFg, themeBg, cell.AttrNone, rightW)
				}
			}

			// Capabilities & Tags
			capY := bodyTop + 6 + len(descLines) + 1
			if capY < startY+modalH-4 {
				capStr := i18n.T("market.capabilities")
				drawString(buf, rightStartX, capY, capStr, keywordFg, themeBg, cell.AttrNone, rightW)
				if len(p.Tags) > 0 {
					tagStr := fmt.Sprintf(i18n.T("market.tags"), strings.Join(p.Tags, ", "))
					drawString(buf, rightStartX, capY+1, tagStr, commentFg, themeBg, cell.AttrNone, rightW)
				}
			}
		}
	}

	// -------------------------------------------------------------
	// TAB 2: Repositories & Custom Servers Manager
	// -------------------------------------------------------------
	if m.ActiveTab == 2 {
		// Left: List of Repositories
		for idx, repo := range m.Repositories {
			rowY := bodyTop + idx*2
			if rowY+1 >= bodyTop+bodyH {
				break
			}
			isCur := (idx == m.SelectedRepo)
			rBg := themeBg
			rFg := themeFg
			attr := cell.AttrNone
			if isCur {
				rBg = selBg
				rFg = selFg
				attr = cell.AttrBold
			}

			check := "[ ]"
			if repo.Enabled {
				check = "[✓]"
			}

			line1 := fmt.Sprintf(" %s %s [%s]", check, repo.Name, repo.Type)
			line1Runes := []rune(line1)
			if len(line1Runes) > leftW-2 {
				line1 = string(line1Runes[:leftW-2])
			}
			for col := 0; col < leftW-2; col++ {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY, ' ', rFg, rBg, attr)
				}
			}
			for col, r := range []rune(line1) {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY, r, rFg, rBg, attr)
				}
			}

			line2 := fmt.Sprintf("     %s", repo.URL)
			line2Runes := []rune(line2)
			if len(line2Runes) > leftW-2 {
				line2 = string(line2Runes[:leftW-5]) + "..."
			}
			for col := 0; col < leftW-2; col++ {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY+1, ' ', themeFg, rBg, cell.AttrNone)
				}
			}
			sFg := commentFg
			if isCur {
				sFg = rFg
			}
			for col, r := range []rune(line2) {
				if startX+1+col < divX {
					buf.SetRune(startX+1+col, rowY+1, r, sFg, rBg, cell.AttrNone)
				}
			}
		}

		// Right: Repository Details & Add Form
		rightStartX := divX + 2
		_ = modalW - leftW - 4

		if m.AddingRepo {
			hStr := fmt.Sprintf(" %s ", i18n.T("market.add_server"))
			if m.EditingRepoID != "" {
				hStr = fmt.Sprintf(" %s ", i18n.T("market.edit_server"))
			}
			drawString(buf, rightStartX, bodyTop, hStr, accentFg, themeBg, cell.AttrBold, modalW-leftW-4)

			f0 := fmt.Sprintf("%s : %s_", i18n.T("market.field_name"), m.AddRepoName)
			f1 := fmt.Sprintf("%s  : %s_", i18n.T("market.field_url"), m.AddRepoURL)
			f2 := fmt.Sprintf("%s : %s_", i18n.T("market.field_type"), m.AddRepoType)

			renderField := func(y int, text string, focused bool) {
				bg := themeBg
				fg := themeFg
				if focused {
					bg = selBg
					fg = selFg
				}
				for i, r := range []rune(text) {
					buf.SetRune(rightStartX+i, y, r, fg, bg, cell.AttrNone)
				}
			}

			renderField(bodyTop+2, f0, m.AddRepoField == 0)
			renderField(bodyTop+4, f1, m.AddRepoField == 1)
			renderField(bodyTop+6, f2, m.AddRepoField == 2)

			help := fmt.Sprintf(" %s ", i18n.T("market.hint_form"))
			drawString(buf, rightStartX, bodyTop+9, help, commentFg, themeBg, cell.AttrNone, modalW-leftW-4)
		} else if m.SelectedRepo >= 0 && m.SelectedRepo < len(m.Repositories) {
			repo := m.Repositories[m.SelectedRepo]

			hStr := fmt.Sprintf(i18n.T("market.server_name"), repo.Name)
			drawString(buf, rightStartX, bodyTop, hStr, accentFg, themeBg, cell.AttrBold, modalW-leftW-4)

			urlStr := fmt.Sprintf(i18n.T("market.server_url"), repo.URL)
			drawString(buf, rightStartX, bodyTop+2, urlStr, themeFg, themeBg, cell.AttrNone, modalW-leftW-4)

			statusText := i18n.T("settings.plugin.enabled")
			if !repo.Enabled {
				statusText = i18n.T("settings.plugin.disabled")
			}
			typeStr := fmt.Sprintf(i18n.T("market.server_type_status"), repo.Type, statusText)
			drawString(buf, rightStartX, bodyTop+3, typeStr, commentFg, themeBg, cell.AttrNone, modalW-leftW-4)

			actStr := fmt.Sprintf(" %s ", i18n.T("market.server_actions"))
			drawString(buf, rightStartX, bodyTop+5, actStr, stringFg, themeBg, cell.AttrBold, modalW-leftW-4)

			if m.RepoTestMsg != "" {
				resLabel := fmt.Sprintf(i18n.T("market.test_status"), m.RepoTestMsg)
				fg := stringFg
				if m.StatusIsError {
					fg = toColor(th.DiagnosticError)
				}
				drawString(buf, rightStartX, bodyTop+7, resLabel, fg, themeBg, cell.AttrBold, modalW-leftW-4)
			}
		}
	}

	// 5. Bottom Status and shortcuts bar
	status := m.StatusMessage
	if status == "" {
		if m.ActiveTab == 1 {
			status = i18n.T("market.status_tab1")
		} else if m.ActiveTab == 2 {
			status = i18n.T("market.status_tab2")
		} else {
			status = i18n.T("market.status_tab0")
		}
	}
	sFg := commentFg
	if m.StatusIsError {
		sFg = toColor(th.DiagnosticError)
	}
	for i, r := range []rune(status) {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY+modalH-1, r, sFg, themeBg, cell.AttrNone)
		}
	}
}

func wrapString(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
		} else if len([]rune(cur))+1+len([]rune(w)) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// HandleClick processes mouse clicks inside and outside the Marketplace window.
func (m *MarketplaceModal) HandleClick(mouseX, mouseY, w, h int) bool {
	if !m.Open {
		return false
	}
	modalW := w - 4
	modalH := h - 2
	if modalW < 40 {
		modalW = w
	}
	if modalH < 14 {
		modalH = h
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// Click outside -> close
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		m.Open = false
		return true
	}

	// Click on tab row (startY+1)
	if mouseY == startY+1 {
		tab1Len := len([]rune(i18n.T("market.tab_browse")))
		tab2Len := len([]rune(i18n.T("market.tab_installed")))
		tab3Len := len([]rune(i18n.T("market.tab_repos")))

		tab1Start := startX + 2
		tab1End := tab1Start + tab1Len
		tab2Start := tab1End + 2
		tab2End := tab2Start + tab2Len
		tab3Start := tab2End + 2
		tab3End := tab3Start + tab3Len

		if mouseX >= tab1Start && mouseX < tab1End {
			m.ActiveTab = 0
			m.SelectedIdx = 0
			m.Refresh()
			return true
		}
		if mouseX >= tab2Start && mouseX < tab2End {
			m.ActiveTab = 1
			m.SelectedIdx = 0
			m.Refresh()
			return true
		}
		if mouseX >= tab3Start && mouseX < tab3End {
			m.ActiveTab = 2
			m.SelectedRepo = 0
			m.Refresh()
			return true
		}
	}

	// Click on search & category row (startY+3)
	if m.ActiveTab != 2 && mouseY == startY+3 {
		searchLabel := i18n.T("market.search")
		sqX := startX + 2 + len([]rune(searchLabel))
		// Click on search input box
		if mouseX >= sqX && mouseX <= sqX+25 {
			m.SearchFocused = true
			return true
		}

		catDisplay := i18n.T("market.cat." + strings.ToLower(m.Categories[m.CatIdx]))
		catLabel := fmt.Sprintf("%s: ◄ %s ►", i18n.T("market.category"), catDisplay)
		catRunes := []rune(catLabel)
		catX := startX + modalW - len(catRunes) - 3
		if mouseX >= catX && mouseX < startX+modalW-1 {
			arrowLeftIdx := -1
			arrowRightIdx := -1
			for idx, r := range catRunes {
				if r == '◄' {
					arrowLeftIdx = idx
				} else if r == '►' {
					arrowRightIdx = idx
				}
			}
			relX := mouseX - catX
			if arrowLeftIdx >= 0 && relX <= arrowLeftIdx+1 {
				if m.CatIdx > 0 {
					m.CatIdx--
				} else {
					m.CatIdx = len(m.Categories) - 1
				}
			} else if arrowRightIdx >= 0 && relX >= arrowRightIdx-1 {
				m.CatIdx = (m.CatIdx + 1) % len(m.Categories)
			} else {
				m.CatIdx = (m.CatIdx + 1) % len(m.Categories)
			}
			m.Refresh()
			return true
		}
	}

	bodyTop := startY + 5
	if m.ActiveTab == 2 {
		bodyTop = startY + 3
	}
	bodyH := modalH - (bodyTop - startY) - 2
	leftW := modalW * 42 / 100
	if leftW < 30 {
		leftW = 30
	}
	divX := startX + leftW

	// Click on left list items
	if mouseX >= startX+1 && mouseX < divX && mouseY >= bodyTop && mouseY < bodyTop+bodyH {
		clickedRow := (mouseY - bodyTop) / 2
		if m.ActiveTab == 2 {
			if clickedRow >= 0 && clickedRow < len(m.Repositories) {
				m.SelectedRepo = clickedRow
				return true
			}
		} else {
			pIdx := m.ScrollOffset + clickedRow
			if pIdx >= 0 && pIdx < len(m.Plugins) {
				m.SelectedIdx = pIdx
				return true
			}
		}
	}

	return true
}

