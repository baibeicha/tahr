package jupyter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OutputType represents the category of cell execution output.
const (
	OutputTypeStream        = "stream"
	OutputTypeExecuteResult = "execute_result"
	OutputTypeDisplayData   = "display_data"
	OutputTypeError         = "error"
)

// CellType represents the notebook cell type.
const (
	CellTypeMarkdown = "markdown"
	CellTypeCode     = "code"
	CellTypeRaw      = "raw"
)

// Output represents an individual output artifact produced by a code cell.
type Output struct {
	OutputType     string                 `json:"output_type"`
	Name           string                 `json:"name,omitempty"`
	Text           []string               `json:"text,omitempty"`
	Data           map[string]interface{} `json:"data,omitempty"`
	ExecutionCount *int                   `json:"execution_count,omitempty"`
	EName          string                 `json:"ename,omitempty"`
	EValue         string                 `json:"evalue,omitempty"`
	Traceback      []string               `json:"traceback,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to accept both string and []string for Text and Traceback.
func (o *Output) UnmarshalJSON(data []byte) error {
	type rawOutput struct {
		OutputType     string                 `json:"output_type"`
		Name           string                 `json:"name,omitempty"`
		Text           json.RawMessage        `json:"text,omitempty"`
		Data           map[string]interface{} `json:"data,omitempty"`
		ExecutionCount *int                   `json:"execution_count,omitempty"`
		EName          string                 `json:"ename,omitempty"`
		EValue         string                 `json:"evalue,omitempty"`
		Traceback      json.RawMessage        `json:"traceback,omitempty"`
		Metadata       map[string]interface{} `json:"metadata,omitempty"`
	}

	var raw rawOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	o.OutputType = raw.OutputType
	o.Name = raw.Name
	o.Data = raw.Data
	o.ExecutionCount = raw.ExecutionCount
	o.EName = raw.EName
	o.EValue = raw.EValue
	o.Metadata = raw.Metadata

	if len(raw.Text) > 0 {
		o.Text = parseFlexibleStringSlice(raw.Text)
	}
	if len(raw.Traceback) > 0 {
		o.Traceback = parseFlexibleStringSlice(raw.Traceback)
	}

	return nil
}

// TextContent returns the text of this output as a single string.
func (o *Output) TextContent() string {
	if len(o.Text) > 0 {
		return strings.Join(o.Text, "")
	}
	if o.Data != nil {
		if plain, ok := o.Data["text/plain"]; ok {
			switch v := plain.(type) {
			case string:
				return v
			case []interface{}:
				var sb strings.Builder
				for _, item := range v {
					sb.WriteString(fmt.Sprint(item))
				}
				return sb.String()
			case []string:
				return strings.Join(v, "")
			default:
				return fmt.Sprint(v)
			}
		}
	}
	if o.OutputType == OutputTypeError {
		if len(o.Traceback) > 0 {
			return strings.Join(o.Traceback, "\n")
		}
		if o.EName != "" || o.EValue != "" {
			return fmt.Sprintf("%s: %s", o.EName, o.EValue)
		}
	}
	return ""
}

// Cell represents an individual code or markdown cell in an .ipynb v4 notebook.
type Cell struct {
	ID             string                 `json:"id,omitempty"`
	CellType       string                 `json:"cell_type"`
	Source         []string               `json:"source"`
	ExecutionCount *int                   `json:"execution_count"`
	Outputs        []Output               `json:"outputs"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// UnmarshalJSON handles flexible parsing of Cell.Source (supporting single string or array of strings).
func (c *Cell) UnmarshalJSON(data []byte) error {
	type rawCell struct {
		ID             string                 `json:"id,omitempty"`
		CellType       string                 `json:"cell_type"`
		Source         json.RawMessage        `json:"source"`
		ExecutionCount *int                   `json:"execution_count"`
		Outputs        []Output               `json:"outputs,omitempty"`
		Metadata       map[string]interface{} `json:"metadata,omitempty"`
	}

	var raw rawCell
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.ID = raw.ID
	c.CellType = raw.CellType
	c.ExecutionCount = raw.ExecutionCount
	if raw.Outputs != nil {
		c.Outputs = raw.Outputs
	} else if c.CellType == CellTypeCode {
		c.Outputs = make([]Output, 0)
	}
	if raw.Metadata != nil {
		c.Metadata = raw.Metadata
	} else {
		c.Metadata = make(map[string]interface{})
	}

	if len(raw.Source) > 0 {
		c.Source = parseFlexibleStringSlice(raw.Source)
	} else {
		c.Source = make([]string, 0)
	}

	return nil
}

// GetSource returns the source code or markdown of the cell as a unified string.
func (c *Cell) GetSource() string {
	if len(c.Source) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, line := range c.Source {
		sb.WriteString(line)
	}
	return sb.String()
}

// SetSource updates the cell source lines from a unified string.
func (c *Cell) SetSource(source string) {
	c.Source = splitSourceLines(source)
}

// AddOutput appends an execution output to this cell.
func (c *Cell) AddOutput(output Output) {
	c.Outputs = append(c.Outputs, output)
}

// ClearOutputs removes all outputs from this cell.
func (c *Cell) ClearOutputs() {
	c.Outputs = make([]Output, 0)
}

// Notebook represents a Jupyter notebook v4 document.
type Notebook struct {
	Cells         []Cell                 `json:"cells"`
	Metadata      map[string]interface{} `json:"metadata"`
	Nbformat      int                    `json:"nbformat"`
	NbformatMinor int                    `json:"nbformat_minor"`
}

// NewNotebook initializes a standard Jupyter v4 notebook.
func NewNotebook() *Notebook {
	return &Notebook{
		Cells: make([]Cell, 0),
		Metadata: map[string]interface{}{
			"language_info": map[string]interface{}{
				"name": "python",
			},
			"kernelspec": map[string]interface{}{
				"name":         "python3",
				"display_name": "Python 3",
				"language":     "python",
			},
		},
		Nbformat:      4,
		NbformatMinor: 5,
	}
}

// ParseNotebook decodes .ipynb JSON data into a Notebook struct.
func ParseNotebook(data []byte) (*Notebook, error) {
	var nb Notebook
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&nb); err != nil {
		return nil, fmt.Errorf("failed to parse notebook json: %w", err)
	}
	if nb.Nbformat == 0 {
		nb.Nbformat = 4
	}
	if nb.NbformatMinor == 0 {
		nb.NbformatMinor = 5
	}
	if nb.Metadata == nil {
		nb.Metadata = make(map[string]interface{})
	}
	return &nb, nil
}

// LoadNotebook reads and parses an .ipynb file from the filesystem.
func LoadNotebook(filePath string) (*Notebook, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file %s: %w", filePath, err)
	}
	return ParseNotebook(data)
}

// Save serializes the notebook back to .ipynb v4 JSON preserving formatting and structure.
func (nb *Notebook) Save(filePath string) error {
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	data, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return fmt.Errorf("failed to serialize notebook: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write notebook file %s: %w", filePath, err)
	}
	return nil
}

// AddCell adds a new cell to the end of the notebook.
func (nb *Notebook) AddCell(cellType, source string) *Cell {
	c := Cell{
		ID:       generateCellID(),
		CellType: cellType,
		Metadata: make(map[string]interface{}),
	}
	c.SetSource(source)
	if cellType == CellTypeCode {
		c.Outputs = make([]Output, 0)
	}
	nb.Cells = append(nb.Cells, c)
	return &nb.Cells[len(nb.Cells)-1]
}

// InsertCell inserts a new cell at the specified index.
func (nb *Notebook) InsertCell(index int, cellType, source string) *Cell {
	if index < 0 {
		index = 0
	}
	if index > len(nb.Cells) {
		index = len(nb.Cells)
	}

	c := Cell{
		ID:       generateCellID(),
		CellType: cellType,
		Metadata: make(map[string]interface{}),
	}
	c.SetSource(source)
	if cellType == CellTypeCode {
		c.Outputs = make([]Output, 0)
	}

	nb.Cells = append(nb.Cells, Cell{})
	copy(nb.Cells[index+1:], nb.Cells[index:])
	nb.Cells[index] = c
	return &nb.Cells[index]
}

// DeleteCell removes the cell at the given index.
func (nb *Notebook) DeleteCell(index int) bool {
	if index < 0 || index >= len(nb.Cells) {
		return false
	}
	nb.Cells = append(nb.Cells[:index], nb.Cells[index+1:]...)
	return true
}

// ClearOutputs clears execution outputs and counts from all cells.
func (nb *Notebook) ClearOutputs() {
	for i := range nb.Cells {
		if nb.Cells[i].CellType == CellTypeCode {
			nb.Cells[i].ClearOutputs()
			nb.Cells[i].ExecutionCount = nil
		}
	}
}

// ClearCellOutputs clears execution outputs from a specific cell.
func (nb *Notebook) ClearCellOutputs(cellIdx int) bool {
	if cellIdx < 0 || cellIdx >= len(nb.Cells) {
		return false
	}
	if nb.Cells[cellIdx].CellType == CellTypeCode {
		nb.Cells[cellIdx].ClearOutputs()
		nb.Cells[cellIdx].ExecutionCount = nil
		return true
	}
	return false
}

// parseFlexibleStringSlice unmarshals either a string or an array of strings.
func parseFlexibleStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try parsing as []string
	var strSlice []string
	if err := json.Unmarshal(raw, &strSlice); err == nil {
		return strSlice
	}

	// Try parsing as []interface{}
	var ifaceSlice []interface{}
	if err := json.Unmarshal(raw, &ifaceSlice); err == nil {
		res := make([]string, 0, len(ifaceSlice))
		for _, item := range ifaceSlice {
			res = append(res, fmt.Sprint(item))
		}
		return res
	}

	// Try parsing as single string
	var singleStr string
	if err := json.Unmarshal(raw, &singleStr); err == nil {
		return splitSourceLines(singleStr)
	}

	return nil
}

// splitSourceLines splits text into Jupyter-style line chunks (preserving newlines).
func splitSourceLines(text string) []string {
	if text == "" {
		return make([]string, 0)
	}

	lines := strings.SplitAfter(text, "\n")
	// If the last element is empty because text ended with \n, drop it
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// generateCellID generates a short unique cell identifier.
var cellCounter uint64

func generateCellID() string {
	cellCounter++
	return fmt.Sprintf("cell-%x", cellCounter)
}
