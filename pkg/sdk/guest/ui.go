package guest

import (
	"bytes"
	"encoding/binary"
)

// Magic & Protocol Constants
const (
	TLVMagic0   byte = 0x54 // 'T'
	TLVMagic1   byte = 0x48 // 'H'
	TLVVersion1 byte = 0x01 // Version 1
)

// NodeType represents visual UI primitives.
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

// TagType identifies attributes in the TLV stream.
type TagType uint16

const (
	TagNodeID           TagType = 0x0001
	TagNodeType         TagType = 0x0002
	TagNodeStyle        TagType = 0x0003
	TagNodeText         TagType = 0x0004
	TagNodeIcon         TagType = 0x0005
	TagNodePlaceholder  TagType = 0x0006
	TagNodeOnClick      TagType = 0x0007
	TagNodeOnInput      TagType = 0x0008
	TagNodeValue        TagType = 0x0009
	TagNodeDisabled     TagType = 0x000A
	TagNodeDataGridCols TagType = 0x0010
	TagNodeDataGridRows TagType = 0x0011
	TagNodeChild        TagType = 0x0020
)

// LayoutStyle defines sizing and margins.
type LayoutStyle struct {
	FlexGrow int16
	Width    int32
	Height   int32
	Padding  [4]int16
}

// Node represents an element in the guest UI hierarchy.
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

// VStack creates a vertical box layout.
func VStack(id string, children ...*Node) *Node {
	return &Node{
		ID:       id,
		Type:     NodeTypeVStack,
		Style:    LayoutStyle{Width: -2, Height: -1},
		Children: children,
	}
}

// HStack creates a horizontal box layout.
func HStack(id string, children ...*Node) *Node {
	return &Node{
		ID:       id,
		Type:     NodeTypeHStack,
		Style:    LayoutStyle{Width: -2, Height: -1},
		Children: children,
	}
}

// Text creates a label element.
func Text(id, text string) *Node {
	return &Node{
		ID:    id,
		Type:  NodeTypeText,
		Text:  text,
		Style: LayoutStyle{Width: -1, Height: -1},
	}
}

// Button creates an interactive button.
func Button(id, text, onClick string) *Node {
	return &Node{
		ID:      id,
		Type:    NodeTypeButton,
		Text:    text,
		OnClick: onClick,
		Style:   LayoutStyle{Width: -1, Height: -1},
	}
}

// SearchInput creates a text input field.
func SearchInput(id, placeholder, value, onInput string) *Node {
	return &Node{
		ID:          id,
		Type:        NodeTypeInput,
		Placeholder: placeholder,
		Value:       value,
		OnInput:     onInput,
		Style:       LayoutStyle{Width: -2, Height: 1},
	}
}

// DataGrid creates a multi-column table.
func DataGrid(id string, cols []string, rows [][]string) *Node {
	return &Node{
		ID:           id,
		Type:         NodeTypeDataGrid,
		DataGridCols: cols,
		DataGridRows: rows,
		Style:        LayoutStyle{Width: -2, Height: -2, FlexGrow: 1},
	}
}

// GraphCanvas creates an interactive DAG Canvas container.
func GraphCanvas(id string) *Node {
	return &Node{
		ID:    id,
		Type:  NodeTypeGraphCanvas,
		Style: LayoutStyle{Width: -2, Height: -2, FlexGrow: 1},
	}
}

// ToBytes encodes the Node tree to the versioned TLV byte buffer.
func ToBytes(root *Node) []byte {
	if root == nil {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteByte(TLVMagic0)
	buf.WriteByte(TLVMagic1)
	buf.WriteByte(TLVVersion1)
	buf.WriteByte(0x00)

	encodeNode(&buf, root)
	return buf.Bytes()
}

func encodeNode(buf *bytes.Buffer, n *Node) {
	if n.ID != "" {
		writeTag(buf, TagNodeID, []byte(n.ID))
	}
	if n.Type != NodeTypeNone {
		var val [2]byte
		binary.LittleEndian.PutUint16(val[:], uint16(n.Type))
		writeTag(buf, TagNodeType, val[:])
	}
	var styleBuf [18]byte
	binary.LittleEndian.PutUint16(styleBuf[0:2], uint16(n.Style.FlexGrow))
	binary.LittleEndian.PutUint32(styleBuf[2:6], uint32(n.Style.Width))
	binary.LittleEndian.PutUint32(styleBuf[6:10], uint32(n.Style.Height))
	binary.LittleEndian.PutUint16(styleBuf[10:12], uint16(n.Style.Padding[0]))
	binary.LittleEndian.PutUint16(styleBuf[12:14], uint16(n.Style.Padding[1]))
	binary.LittleEndian.PutUint16(styleBuf[14:16], uint16(n.Style.Padding[2]))
	binary.LittleEndian.PutUint16(styleBuf[16:18], uint16(n.Style.Padding[3]))
	writeTag(buf, TagNodeStyle, styleBuf[:])

	if n.Text != "" {
		writeTag(buf, TagNodeText, []byte(n.Text))
	}
	if n.Placeholder != "" {
		writeTag(buf, TagNodePlaceholder, []byte(n.Placeholder))
	}
	if n.Value != "" {
		writeTag(buf, TagNodeValue, []byte(n.Value))
	}
	if n.OnClick != "" {
		writeTag(buf, TagNodeOnClick, []byte(n.OnClick))
	}
	if n.OnInput != "" {
		writeTag(buf, TagNodeOnInput, []byte(n.OnInput))
	}
	if len(n.DataGridCols) > 0 {
		var colBuf bytes.Buffer
		var count [2]byte
		binary.LittleEndian.PutUint16(count[:], uint16(len(n.DataGridCols)))
		colBuf.Write(count[:])
		for _, col := range n.DataGridCols {
			var lenBuf [2]byte
			binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(col)))
			colBuf.Write(lenBuf[:])
			colBuf.WriteString(col)
		}
		writeTag(buf, TagNodeDataGridCols, colBuf.Bytes())
	}
	if len(n.DataGridRows) > 0 {
		var rowBuf bytes.Buffer
		var count [4]byte
		binary.LittleEndian.PutUint32(count[:], uint32(len(n.DataGridRows)))
		rowBuf.Write(count[:])
		for _, row := range n.DataGridRows {
			var colCount [2]byte
			binary.LittleEndian.PutUint16(colCount[:], uint16(len(row)))
			rowBuf.Write(colCount[:])
			for _, cell := range row {
				var lenBuf [2]byte
				binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(cell)))
				rowBuf.Write(lenBuf[:])
				rowBuf.WriteString(cell)
			}
		}
		writeTag(buf, TagNodeDataGridRows, rowBuf.Bytes())
	}

	for _, child := range n.Children {
		if child == nil {
			continue
		}
		var childBuf bytes.Buffer
		encodeNode(&childBuf, child)
		writeTag(buf, TagNodeChild, childBuf.Bytes())
	}
}

func writeTag(buf *bytes.Buffer, t TagType, val []byte) {
	var header [6]byte
	binary.LittleEndian.PutUint16(header[0:2], uint16(t))
	binary.LittleEndian.PutUint32(header[2:6], uint32(len(val)))
	buf.Write(header[:])
	buf.Write(val)
}
