package tahr_sdk

// ToastLevel represents the severity of a notification card.
type ToastLevel int

const (
	ToastLevelInfo ToastLevel = iota
	ToastLevelSuccess
	ToastLevelWarn
	ToastLevelError
)

// Position represents a 0-based line and column in the document.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Selection represents an anchor and head cursor position.
type Selection struct {
	Anchor Position `json:"anchor"`
	Head   Position `json:"head"`
}

// Range represents a text interval between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// ToolWindowDef describes a custom tool window provided by a plugin.
type ToolWindowDef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Icon     string `json:"icon"`
	Position string `json:"position"` // "left", "bottom", "right"
}

// VirtualTextKind defines presentation style of virtual text decorations.
type VirtualTextKind int

const (
	VirtualTextInlay VirtualTextKind = iota
	VirtualTextEndOfLine
	VirtualTextConceal
)

// VirtualTextItem represents an inline or end-of-line decoration.
type VirtualTextItem struct {
	ID       string          `json:"id"`
	Source   string          `json:"source"`
	Kind     VirtualTextKind `json:"kind"`
	Line     int             `json:"line"`
	Col      int             `json:"col"`
	EndCol   int             `json:"end_col,omitempty"`
	Text     string          `json:"text"`
	FgColor  uint32          `json:"fg_color,omitempty"`
	BgColor  uint32          `json:"bg_color,omitempty"`
	Priority int             `json:"priority,omitempty"`
}

// GutterMarker represents an indicator glyph drawn in the line-number gutter.
type GutterMarker struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Glyph   string `json:"glyph"`
	FgColor uint32 `json:"fg_color,omitempty"`
}

// Bookmark represents a saved line marker across the workspace.
type Bookmark struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Label    string `json:"label,omitempty"`
}

// CodeLensItem represents an interactive action button above a code line.
type CodeLensItem struct {
	Range     Range  `json:"range"`
	CommandID string `json:"command_id"`
	Title     string `json:"title"`
}

// DiagnosticSeverity indicates problem severity in the editor.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError DiagnosticSeverity = iota + 1
	DiagnosticSeverityWarning
	DiagnosticSeverityInformation
	DiagnosticSeverityHint
)

// DiagnosticItem represents an issue reported by a plugin or linter.
type DiagnosticItem struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity"`
	Message  string             `json:"message"`
	Source   string             `json:"source"`
}

// DataGridColumn defines a column header in a DataGrid.
type DataGridColumn struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Width    int    `json:"width,omitempty"`
	Sortable bool   `json:"sortable,omitempty"`
}

// DataGridDef represents tabular data rendered with high FPS in TUI/GUI.
type DataGridDef struct {
	ID        string           `json:"id"`
	Columns   []DataGridColumn `json:"columns"`
	Rows      [][]string       `json:"rows"`
	TotalRows int              `json:"total_rows"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
	SortCol   string           `json:"sort_col,omitempty"`
	SortDesc  bool             `json:"sort_desc,omitempty"`
}

// TreeNode represents a hierarchical item in a TreeView.
type TreeNode struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Icon     string            `json:"icon,omitempty"`
	Badge    string            `json:"badge,omitempty"`
	Expanded bool              `json:"expanded,omitempty"`
	FilePath string            `json:"file_path,omitempty"`
	Line     int               `json:"line,omitempty"`
	Children []TreeNode        `json:"children,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
}

// TreeViewDef represents a tree structure displayed in a tool window.
type TreeViewDef struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Root  []TreeNode `json:"root"`
}

// ChatMessage represents a single message in an AI conversation stream.
type ChatMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"` // "user", "assistant", "system"
	Content   string `json:"content"`
	Status    string `json:"status,omitempty"` // "streaming", "done", "error"
	Timestamp int64  `json:"timestamp,omitempty"`
}

// ChatViewDef represents an AI chat stream session.
type ChatViewDef struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Messages []ChatMessage `json:"messages"`
}

// SplitViewDef represents a two-pane side-by-side or stacked comparative view.
type SplitViewDef struct {
	ID          string `json:"id"`
	Orientation string `json:"orientation"` // "horizontal", "vertical"
	LeftTitle   string `json:"left_title"`
	RightTitle  string `json:"right_title"`
	LeftText    string `json:"left_text"`
	RightText   string `json:"right_text"`
}

// NotebookCellType defines whether a cell is code, markdown, or raw.
type NotebookCellType string

const (
	CellTypeCode     NotebookCellType = "code"
	CellTypeMarkdown NotebookCellType = "markdown"
	CellTypeRaw      NotebookCellType = "raw"
)

// NotebookCellOutput represents standard mime-type or stream outputs from cell execution.
type NotebookCellOutput struct {
	OutputType string            `json:"output_type"` // "stream", "execute_result", "display_data", "error"
	Name       string            `json:"name,omitempty"` // "stdout", "stderr"
	Text       string            `json:"text,omitempty"`
	Data       map[string]string `json:"data,omitempty"` // e.g. "text/plain", "image/png"
	Ename      string            `json:"ename,omitempty"`
	Evalue     string            `json:"evalue,omitempty"`
	Traceback  []string          `json:"traceback,omitempty"`
}

// NotebookCell represents an individual executable or documentation block in a notebook.
type NotebookCell struct {
	ID             string               `json:"id"`
	CellType       NotebookCellType     `json:"cell_type"`
	Source         string               `json:"source"`
	ExecutionCount int                  `json:"execution_count,omitempty"`
	Outputs        []NotebookCellOutput `json:"outputs,omitempty"`
	Status         string               `json:"status,omitempty"` // "idle", "busy", "queued", "error"
}

// NotebookDef represents an interactive Jupyter notebook document (.ipynb).
type NotebookDef struct {
	ID         string         `json:"id"`
	FilePath   string         `json:"file_path"`
	KernelName string         `json:"kernel_name"` // e.g. "python3", "gophernotes", "ir"
	Language   string         `json:"language"`    // "python", "go", "r", "julia"
	Cells      []NotebookCell `json:"cells"`
}

// ConnectionProfile represents a stored or auto-detected remote connection configuration.
type ConnectionProfile struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"` // "ssh", "postgres", "mysql", "redis", "kafka", "rabbitmq", "k8s", "s3", "nats", "clickhouse"
	Name      string            `json:"name"`
	Host      string            `json:"host"`
	Port      int               `json:"port"`
	User      string            `json:"user,omitempty"`
	Database  string            `json:"database,omitempty"`
	SecretRef string            `json:"secret_ref,omitempty"` // Key reference stored in Conceal engine
	Options   map[string]string `json:"options,omitempty"`
	Status    string            `json:"status,omitempty"` // "connected", "disconnected", "error"
}

