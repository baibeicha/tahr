package sdk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrInvalidMagic is returned when the binary buffer does not start with 'TH'.
	ErrInvalidMagic = errors.New("invalid TLV magic: expected 'TH' (0x54 0x48)")
	// ErrUnsupportedVersion is returned when the payload version exceeds current decoder support.
	ErrUnsupportedVersion = errors.New("unsupported TLV protocol version")
	// ErrUnexpectedEOF is returned when buffer ends before tag completes.
	ErrUnexpectedEOF = errors.New("unexpected end of TLV buffer")
)

// TagHeader represents the 6-byte prefix of every TLV entry.
type TagHeader struct {
	Type   TagType
	Length uint32
}

// EncodeBinaryTree serializes a Node tree into a versioned binary TLV buffer.
func EncodeBinaryTree(root *Node) ([]byte, error) {
	if root == nil {
		return nil, errors.New("cannot encode nil node")
	}

	var buf bytes.Buffer

	// 1. Write 4-byte stream header: [Magic0, Magic1, Version, Flags]
	buf.WriteByte(TLVMagic0)
	buf.WriteByte(TLVMagic1)
	buf.WriteByte(TLVVersion1)
	buf.WriteByte(TLVFlagNone)

	// 2. Encode root node payload
	nodeBytes, err := encodeNode(root)
	if err != nil {
		return nil, fmt.Errorf("encode node: %w", err)
	}
	buf.Write(nodeBytes)

	return buf.Bytes(), nil
}

// encodeNode encodes a single node's attributes and its children as sequential TLV tags.
func encodeNode(n *Node) ([]byte, error) {
	var buf bytes.Buffer

	// TagNodeID
	if n.ID != "" {
		writeStringTag(&buf, TagNodeID, n.ID)
	}

	// TagNodeType
	if n.Type != NodeTypeNone {
		var tagVal [2]byte
		binary.LittleEndian.PutUint16(tagVal[:], uint16(n.Type))
		writeTag(&buf, TagNodeType, tagVal[:])
	}

	// TagNodeStyle (18 bytes: FlexGrow int16, Width int32, Height int32, Padding [4]int16)
	var styleBuf [18]byte
	binary.LittleEndian.PutUint16(styleBuf[0:2], uint16(n.Style.FlexGrow))
	binary.LittleEndian.PutUint32(styleBuf[2:6], uint32(n.Style.Width))
	binary.LittleEndian.PutUint32(styleBuf[6:10], uint32(n.Style.Height))
	binary.LittleEndian.PutUint16(styleBuf[10:12], uint16(n.Style.Padding[0]))
	binary.LittleEndian.PutUint16(styleBuf[12:14], uint16(n.Style.Padding[1]))
	binary.LittleEndian.PutUint16(styleBuf[14:16], uint16(n.Style.Padding[2]))
	binary.LittleEndian.PutUint16(styleBuf[16:18], uint16(n.Style.Padding[3]))
	writeTag(&buf, TagNodeStyle, styleBuf[:])

	// TagNodeText
	if n.Text != "" {
		writeStringTag(&buf, TagNodeText, n.Text)
	}

	// TagNodeIcon
	if n.Icon != "" {
		writeStringTag(&buf, TagNodeIcon, n.Icon)
	}

	// TagNodePlaceholder
	if n.Placeholder != "" {
		writeStringTag(&buf, TagNodePlaceholder, n.Placeholder)
	}

	// TagNodeValue
	if n.Value != "" {
		writeStringTag(&buf, TagNodeValue, n.Value)
	}

	// TagNodeOnClick
	if n.OnClick != "" {
		writeStringTag(&buf, TagNodeOnClick, n.OnClick)
	}

	// TagNodeOnInput
	if n.OnInput != "" {
		writeStringTag(&buf, TagNodeOnInput, n.OnInput)
	}

	// TagNodeDisabled
	if n.Disabled {
		writeTag(&buf, TagNodeDisabled, []byte{1})
	}

	// TagNodeDataGridCols
	if len(n.DataGridCols) > 0 {
		var colBuf bytes.Buffer
		var count [2]byte
		binary.LittleEndian.PutUint16(count[:], uint16(len(n.DataGridCols)))
		colBuf.Write(count[:])
		for _, col := range n.DataGridCols {
			writePrefixedString(&colBuf, col)
		}
		writeTag(&buf, TagNodeDataGridCols, colBuf.Bytes())
	}

	// TagNodeDataGridRows
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
				writePrefixedString(&rowBuf, cell)
			}
		}
		writeTag(&buf, TagNodeDataGridRows, rowBuf.Bytes())
	}

	// TagNodeChild for each child
	for _, child := range n.Children {
		if child == nil {
			continue
		}
		childBytes, err := encodeNode(child)
		if err != nil {
			return nil, err
		}
		writeTag(&buf, TagNodeChild, childBytes)
	}

	return buf.Bytes(), nil
}

// DecodeBinaryTree parses a versioned binary TLV buffer into a Node tree.
func DecodeBinaryTree(buf []byte) (*Node, error) {
	if len(buf) < 4 {
		return nil, ErrUnexpectedEOF
	}

	// 1. Verify Magic Header
	if buf[0] != TLVMagic0 || buf[1] != TLVMagic1 {
		return nil, fmt.Errorf("%w: got [0x%02X, 0x%02X]", ErrInvalidMagic, buf[0], buf[1])
	}

	// 2. Verify Protocol Version
	ver := buf[2]
	if ver > TLVVersion1 {
		return nil, fmt.Errorf("%w: got %d, max supported %d", ErrUnsupportedVersion, ver, TLVVersion1)
	}

	// 3. Decode Root Node from payload
	root, _, err := decodeNode(buf[4:])
	if err != nil {
		return nil, err
	}
	return root, nil
}

// decodeNode decodes a node from a byte slice. It returns the node, the number of bytes consumed, and any error.
func decodeNode(data []byte) (*Node, int, error) {
	node := &Node{}
	offset := 0

	for offset+6 <= len(data) {
		tagType := TagType(binary.LittleEndian.Uint16(data[offset : offset+2]))
		tagLen := binary.LittleEndian.Uint32(data[offset+2 : offset+6])
		offset += 6

		if uint64(offset)+uint64(tagLen) > uint64(len(data)) {
			return nil, 0, ErrUnexpectedEOF
		}

		tagValue := data[offset : offset+int(tagLen)]
		offset += int(tagLen)

		switch tagType {
		case TagNodeID:
			node.ID = string(tagValue)
		case TagNodeType:
			if len(tagValue) >= 2 {
				node.Type = NodeType(binary.LittleEndian.Uint16(tagValue))
			}
		case TagNodeStyle:
			if len(tagValue) >= 16 {
				node.Style.FlexGrow = int16(binary.LittleEndian.Uint16(tagValue[0:2]))
				node.Style.Width = int32(binary.LittleEndian.Uint32(tagValue[2:6]))
				node.Style.Height = int32(binary.LittleEndian.Uint32(tagValue[6:10]))
				node.Style.Padding[0] = int16(binary.LittleEndian.Uint16(tagValue[10:12]))
				node.Style.Padding[1] = int16(binary.LittleEndian.Uint16(tagValue[12:14]))
				node.Style.Padding[2] = int16(binary.LittleEndian.Uint16(tagValue[14:16]))
			}
			if len(tagValue) >= 18 {
				node.Style.Padding[3] = int16(binary.LittleEndian.Uint16(tagValue[16:18]))
			}
		case TagNodeText:
			node.Text = string(tagValue)
		case TagNodeIcon:
			node.Icon = string(tagValue)
		case TagNodePlaceholder:
			node.Placeholder = string(tagValue)
		case TagNodeValue:
			node.Value = string(tagValue)
		case TagNodeOnClick:
			node.OnClick = string(tagValue)
		case TagNodeOnInput:
			node.OnInput = string(tagValue)
		case TagNodeDisabled:
			if len(tagValue) > 0 {
				node.Disabled = tagValue[0] == 1
			}
		case TagNodeDataGridCols:
			cols, err := decodeStringList(tagValue)
			if err == nil {
				node.DataGridCols = cols
			}
		case TagNodeDataGridRows:
			rows, err := decodeGridRows(tagValue)
			if err == nil {
				node.DataGridRows = rows
			}
		case TagNodeChild:
			child, _, err := decodeNode(tagValue)
			if err != nil {
				return nil, 0, err
			}
			node.Children = append(node.Children, child)
		default:
			// Forward compatibility: Unknown tag safely skipped by tagLen!
		}
	}

	return node, offset, nil
}

func writeTag(buf *bytes.Buffer, t TagType, val []byte) {
	var header [6]byte
	binary.LittleEndian.PutUint16(header[0:2], uint16(t))
	binary.LittleEndian.PutUint32(header[2:6], uint32(len(val)))
	buf.Write(header[:])
	buf.Write(val)
}

func writeStringTag(buf *bytes.Buffer, t TagType, str string) {
	writeTag(buf, t, []byte(str))
}

func writePrefixedString(buf *bytes.Buffer, str string) {
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(str)))
	buf.Write(lenBuf[:])
	buf.WriteString(str)
}

func decodeStringList(data []byte) ([]string, error) {
	if len(data) < 2 {
		return nil, ErrUnexpectedEOF
	}
	count := int(binary.LittleEndian.Uint16(data[0:2]))
	offset := 2
	list := make([]string, 0, count)

	for i := 0; i < count; i++ {
		if offset+2 > len(data) {
			return nil, ErrUnexpectedEOF
		}
		strLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+strLen > len(data) {
			return nil, ErrUnexpectedEOF
		}
		list = append(list, string(data[offset:offset+strLen]))
		offset += strLen
	}
	return list, nil
}

func decodeGridRows(data []byte) ([][]string, error) {
	if len(data) < 4 {
		return nil, ErrUnexpectedEOF
	}
	rowCount := int(binary.LittleEndian.Uint32(data[0:4]))
	offset := 4
	rows := make([][]string, 0, rowCount)

	for r := 0; r < rowCount; r++ {
		if offset+2 > len(data) {
			return nil, ErrUnexpectedEOF
		}
		colCount := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		row := make([]string, 0, colCount)
		for c := 0; c < colCount; c++ {
			if offset+2 > len(data) {
				return nil, ErrUnexpectedEOF
			}
			strLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+strLen > len(data) {
				return nil, ErrUnexpectedEOF
			}
			row = append(row, string(data[offset:offset+strLen]))
			offset += strLen
		}
		rows = append(rows, row)
	}
	return rows, nil
}
