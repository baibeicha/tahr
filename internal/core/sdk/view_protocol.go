package sdk

// TLV Protocol Header Constants
const (
	TLVMagic0   byte = 0x54 // 'T'
	TLVMagic1   byte = 0x48 // 'H'
	TLVVersion1 byte = 0x01 // Version 1
	TLVFlagNone byte = 0x00
)

// NodeType represents the kind of visual UI component.
type NodeType uint16

const (
	NodeTypeNone        NodeType = 0x0000
	NodeTypeVStack      NodeType = 0x0001
	NodeTypeHStack      NodeType = 0x0002
	NodeTypeText        NodeType = 0x0003
	NodeTypeButton      NodeType = 0x0004
	NodeTypeInput       NodeType = 0x0005
	NodeTypeDataGrid    NodeType = 0x0006
	NodeTypeTreeView    NodeType = 0x0007
	NodeTypeGraphCanvas NodeType = 0x0008
)

// String returns a readable representation of the node type.
func (nt NodeType) String() string {
	switch nt {
	case NodeTypeVStack:
		return "VStack"
	case NodeTypeHStack:
		return "HStack"
	case NodeTypeText:
		return "Text"
	case NodeTypeButton:
		return "Button"
	case NodeTypeInput:
		return "Input"
	case NodeTypeDataGrid:
		return "DataGrid"
	case NodeTypeTreeView:
		return "TreeView"
	case NodeTypeGraphCanvas:
		return "GraphCanvas"
	default:
		return "Unknown"
	}
}

// TagType identifies individual attributes in the Tag-Length-Value payload.
type TagType uint16

const (
	TagNodeID           TagType = 0x0001 // string
	TagNodeType         TagType = 0x0002 // uint16
	TagNodeStyle        TagType = 0x0003 // LayoutStyle (16 bytes)
	TagNodeText         TagType = 0x0004 // string
	TagNodeIcon         TagType = 0x0005 // string
	TagNodePlaceholder  TagType = 0x0006 // string
	TagNodeOnClick      TagType = 0x0007 // string action ID
	TagNodeOnInput      TagType = 0x0008 // string action ID
	TagNodeValue        TagType = 0x0009 // string current input/state value
	TagNodeDisabled     TagType = 0x000A // uint8 (0 or 1)
	TagNodeDataGridCols TagType = 0x0010 // uint16 count + length-prefixed strings
	TagNodeDataGridRows TagType = 0x0011 // uint32 count + rows
	TagNodeChild        TagType = 0x0020 // nested node block
)

// Sizing constants for LayoutStyle.
const (
	SizeAuto int32 = -1
	SizeFill int32 = -2
)

// LayoutStyle defines the layout geometry and alignment.
type LayoutStyle struct {
	FlexGrow int16
	Width    int32    // SizeAuto, SizeFill, or explicit cell/pixel width
	Height   int32    // SizeAuto, SizeFill, or explicit cell/pixel height
	Padding  [4]int16 // Top, Right, Bottom, Left
}

// Node represents a single element in the declarative UI hierarchy.
type Node struct {
	ID           string
	Type         NodeType
	Style        LayoutStyle
	Text         string
	Icon         string
	Placeholder  string
	Value        string
	OnClick      string
	OnInput      string
	Disabled     bool
	DataGridCols []string
	DataGridRows [][]string
	Children     []*Node
}

// NewVStack creates a vertical layout container node.
func NewVStack(id string, children ...*Node) *Node {
	return &Node{
		ID:       id,
		Type:     NodeTypeVStack,
		Style:    LayoutStyle{Width: SizeFill, Height: SizeAuto},
		Children: children,
	}
}

// NewHStack creates a horizontal layout container node.
func NewHStack(id string, children ...*Node) *Node {
	return &Node{
		ID:       id,
		Type:     NodeTypeHStack,
		Style:    LayoutStyle{Width: SizeFill, Height: SizeAuto},
		Children: children,
	}
}

// NewText creates a static text display node.
func NewText(id, text string) *Node {
	return &Node{
		ID:    id,
		Type:  NodeTypeText,
		Text:  text,
		Style: LayoutStyle{Width: SizeAuto, Height: SizeAuto},
	}
}

// NewButton creates an interactive button node.
func NewButton(id, label, onClick string) *Node {
	return &Node{
		ID:      id,
		Type:    NodeTypeButton,
		Text:    label,
		OnClick: onClick,
		Style:   LayoutStyle{Width: SizeAuto, Height: SizeAuto},
	}
}

// NewInput creates an editable text input node.
func NewInput(id, placeholder, value, onInput string) *Node {
	return &Node{
		ID:          id,
		Type:        NodeTypeInput,
		Placeholder: placeholder,
		Value:       value,
		OnInput:     onInput,
		Style:       LayoutStyle{Width: SizeFill, Height: 1},
	}
}

// NewDataGrid creates a multi-column tabular data grid node.
func NewDataGrid(id string, cols []string, rows [][]string) *Node {
	return &Node{
		ID:           id,
		Type:         NodeTypeDataGrid,
		DataGridCols: cols,
		DataGridRows: rows,
		Style:        LayoutStyle{Width: SizeFill, Height: SizeFill, FlexGrow: 1},
	}
}

// NewGraphCanvas creates a placeholder container for an interactive DAG Canvas.
func NewGraphCanvas(id string) *Node {
	return &Node{
		ID:    id,
		Type:  NodeTypeGraphCanvas,
		Style: LayoutStyle{Width: SizeFill, Height: SizeFill, FlexGrow: 1},
	}
}
