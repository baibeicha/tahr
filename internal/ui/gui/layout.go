package gui

// RectF represents a 2D bounding rectangle in pixel coordinates.
type RectF struct {
	X      float32
	Y      float32
	Width  float32
	Height float32
}

// Contains checks if point (px, py) is within the rectangle.
func (r RectF) Contains(px, py float32) bool {
	return px >= r.X && px <= r.X+r.Width && py >= r.Y && py <= r.Y+r.Height
}

// GuiLayout stores computed pixel regions for all top-level IDE components.
type GuiLayout struct {
	WindowWidth  float32
	WindowHeight float32

	// Activity bar & sidebar
	ActivityBar RectF
	Sidebar     RectF
	SidebarOpen bool

	// Split editor panes
	EditorArea RectF
	Panes      []RectF

	// Bottom panel (Terminal / DAP)
	BottomPanel RectF
	BottomOpen  bool

	// Status bar
	StatusBar RectF

	// Floating modals
	OmnibarModal  RectF
	QuickFixModal RectF
}

// ComputeLayout calculates immediate-mode bounding boxes for the given window geometry.
func ComputeLayout(w, h float32, sidebarOpen bool, sidebarWidth float32, bottomOpen bool, bottomHeight float32, splitCount int) GuiLayout {
	l := GuiLayout{
		WindowWidth:  w,
		WindowHeight: h,
		SidebarOpen:  sidebarOpen,
		BottomOpen:   bottomOpen,
	}

	activityBarWidth := float32(48.0)
	statusBarHeight := float32(24.0)

	// Activity Bar on the left
	l.ActivityBar = RectF{
		X:      0,
		Y:      0,
		Width:  activityBarWidth,
		Height: h - statusBarHeight,
	}

	curX := activityBarWidth

	// Optional Sidebar (File Tree)
	if sidebarOpen {
		if sidebarWidth <= 0 {
			sidebarWidth = 240.0
		}
		l.Sidebar = RectF{
			X:      curX,
			Y:      0,
			Width:  sidebarWidth,
			Height: h - statusBarHeight,
		}
		curX += sidebarWidth
	}

	// Bottom Panel
	bottomPanelY := h - statusBarHeight
	if bottomOpen {
		if bottomHeight <= 0 {
			bottomHeight = 220.0
		}
		bottomPanelY = h - statusBarHeight - bottomHeight
		l.BottomPanel = RectF{
			X:      curX,
			Y:      bottomPanelY,
			Width:  w - curX,
			Height: bottomHeight,
		}
	}

	// Main Editor Area
	editorW := w - curX
	editorH := bottomPanelY
	l.EditorArea = RectF{
		X:      curX,
		Y:      0,
		Width:  editorW,
		Height: editorH,
	}

	// Split Panes (1 to 6)
	if splitCount < 1 {
		splitCount = 1
	}
	if splitCount > 6 {
		splitCount = 6
	}

	l.Panes = make([]RectF, splitCount)
	switch splitCount {
	case 1:
		l.Panes[0] = l.EditorArea
	case 2:
		colW := editorW / 2.0
		l.Panes[0] = RectF{X: curX, Y: 0, Width: colW, Height: editorH}
		l.Panes[1] = RectF{X: curX + colW, Y: 0, Width: colW, Height: editorH}
	case 3:
		colW := editorW / 3.0
		for i := 0; i < 3; i++ {
			l.Panes[i] = RectF{X: curX + float32(i)*colW, Y: 0, Width: colW, Height: editorH}
		}
	case 4:
		colW := editorW / 2.0
		rowH := editorH / 2.0
		l.Panes[0] = RectF{X: curX, Y: 0, Width: colW, Height: rowH}
		l.Panes[1] = RectF{X: curX + colW, Y: 0, Width: colW, Height: rowH}
		l.Panes[2] = RectF{X: curX, Y: rowH, Width: colW, Height: rowH}
		l.Panes[3] = RectF{X: curX + colW, Y: rowH, Width: colW, Height: rowH}
	default:
		// 5 or 6 panes: 3 columns x 2 rows
		colW := editorW / 3.0
		rowH := editorH / 2.0
		for i := 0; i < splitCount; i++ {
			c := i % 3
			r := i / 3
			l.Panes[i] = RectF{X: curX + float32(c)*colW, Y: float32(r)*rowH, Width: colW, Height: rowH}
		}
	}

	// Status Bar at the bottom
	l.StatusBar = RectF{
		X:      0,
		Y:      h - statusBarHeight,
		Width:  w,
		Height: statusBarHeight,
	}

	// Omnibar modal centered
	omniW := float32(600.0)
	if omniW > w-40 {
		omniW = w - 40
	}
	l.OmnibarModal = RectF{
		X:      (w - omniW) / 2.0,
		Y:      60.0,
		Width:  omniW,
		Height: 320.0,
	}

	return l
}
