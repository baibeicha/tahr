// Package buffer provides an Augmented AVL Rope data structure storing both
// byte lengths (weight) and newline counts (lines) in each node.
// This supports O(log N) line-to-byte and byte-to-line seeking, zero-allocation
// chunk referencing, and safe CRLF normalization.
package buffer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	// ErrLineOutOfBounds indicates lineIdx < 0 or lineIdx >= TotalLines().
	ErrLineOutOfBounds = errors.New("line index out of bounds")

	// ErrOffsetOutOfBounds indicates offset < 0 or offset > TotalBytes().
	ErrOffsetOutOfBounds = errors.New("byte offset out of bounds")
)

const (
	// MinChunkSize is the lower bound below which adjacent leaves may be merged.
	MinChunkSize = 512

	// MaxChunkSize is the upper bound above which leaves are split into halves.
	MaxChunkSize = 2048
)

// Node represents a node in the augmented AVL rope.
// When isLeaf() is true (left == nil && right == nil), val stores the text chunk.
// When isLeaf() is false, val is nil, and left/right point to child subtrees.
type Node struct {
	left   *Node  // Left child subtree
	right  *Node  // Right child subtree
	weight int    // Total byte length of this subtree
	lines  int    // Total newline ('\n') count in this subtree
	height int16  // Height of this subtree (leaf height == 1)
	val    []byte // Text payload (non-nil ONLY for leaf nodes)
}

func newLeaf(b []byte) *Node {
	n := &Node{
		weight: len(b),
		lines:  bytes.Count(b, []byte{'\n'}),
		height: 1,
		val:    b,
	}
	return n
}

func (n *Node) isLeaf() bool {
	return n.left == nil && n.right == nil
}

func nodeHeight(n *Node) int16 {
	if n == nil {
		return 0
	}
	return n.height
}

func nodeWeight(n *Node) int {
	if n == nil {
		return 0
	}
	return n.weight
}

func nodeLines(n *Node) int {
	if n == nil {
		return 0
	}
	return n.lines
}

func (n *Node) update() {
	if n.isLeaf() {
		n.height = 1
		n.weight = len(n.val)
		n.lines = bytes.Count(n.val, []byte{'\n'})
		return
	}
	hl := nodeHeight(n.left)
	hr := nodeHeight(n.right)
	if hl > hr {
		n.height = hl + 1
	} else {
		n.height = hr + 1
	}
	n.weight = nodeWeight(n.left) + nodeWeight(n.right)
	n.lines = nodeLines(n.left) + nodeLines(n.right)
}

func (n *Node) balanceFactor() int {
	if n == nil {
		return 0
	}
	return int(nodeHeight(n.left)) - int(nodeHeight(n.right))
}

func rotateRight(y *Node) *Node {
	x := y.left
	y.left = x.right
	x.right = y
	y.update()
	x.update()
	return x
}

func rotateLeft(x *Node) *Node {
	y := x.right
	x.right = y.left
	y.left = x
	x.update()
	y.update()
	return y
}

func rebalance(n *Node) *Node {
	if n == nil || n.isLeaf() {
		return n
	}
	n.update()
	bf := n.balanceFactor()
	if bf > 1 {
		if n.left != nil && n.left.balanceFactor() < 0 {
			n.left = rotateLeft(n.left)
		}
		return rotateRight(n)
	} else if bf < -1 {
		if n.right != nil && n.right.balanceFactor() > 0 {
			n.right = rotateRight(n.right)
		}
		return rotateLeft(n)
	}
	return n
}

// Concat joins two AVL rope subtrees t1 and t2 in O(|height(t1) - height(t2)|) time.
func Concat(t1, t2 *Node) *Node {
	if t1 == nil {
		return t2
	}
	if t2 == nil {
		return t1
	}

	// Leaf merge optimization: if both are leaves and combined length fits MaxChunkSize
	if t1.isLeaf() && t2.isLeaf() && (t1.weight+t2.weight) <= MaxChunkSize {
		merged := make([]byte, t1.weight+t2.weight)
		copy(merged, t1.val)
		copy(merged[t1.weight:], t2.val)
		return newLeaf(merged)
	}

	h1 := nodeHeight(t1)
	h2 := nodeHeight(t2)

	if h1 > h2+1 {
		t1.right = Concat(t1.right, t2)
		return rebalance(t1)
	} else if h2 > h1+1 {
		t2.left = Concat(t1, t2.left)
		return rebalance(t2)
	} else {
		n := &Node{
			left:  t1,
			right: t2,
		}
		n.update()
		return n
	}
}

// Split divides an AVL rope subtree n at byte offset into left [0, offset) and right [offset, weight).
func Split(n *Node, offset int) (*Node, *Node) {
	if n == nil {
		return nil, nil
	}
	if offset <= 0 {
		return nil, n
	}
	if offset >= n.weight {
		return n, nil
	}

	if n.isLeaf() {
		leftVal := make([]byte, offset)
		copy(leftVal, n.val[:offset])

		rightVal := make([]byte, len(n.val)-offset)
		copy(rightVal, n.val[offset:])

		return newLeaf(leftVal), newLeaf(rightVal)
	}

	leftWeight := nodeWeight(n.left)
	if offset < leftWeight {
		l1, l2 := Split(n.left, offset)
		return l1, Concat(l2, n.right)
	} else if offset > leftWeight {
		r1, r2 := Split(n.right, offset-leftWeight)
		return Concat(n.left, r1), r2
	} else {
		// Exact split along child boundary
		return n.left, n.right
	}
}

// Rope is the top-level augmented AVL rope buffer.
type Rope struct {
	root    *Node
	hasCRLF bool
	scratch []byte // Reusable scratch buffer to avoid heap allocations
}

// NewRope creates an empty Rope.
func NewRope() *Rope {
	return &Rope{
		root:    nil,
		hasCRLF: false,
		scratch: make([]byte, 0, 1024),
	}
}

// TotalBytes returns the total length of the buffer in bytes.
func (r *Rope) TotalBytes() int {
	return nodeWeight(r.root)
}

// TotalLines returns the total line count.
// An empty buffer has 1 line (Line 0, empty string).
// In general, TotalLines = newlineCount + 1.
func (r *Rope) TotalLines() int {
	if r.root == nil {
		return 1
	}
	return r.root.lines + 1
}

// HasCRLF reports whether the buffer was loaded from a file with CRLF line endings.
func (r *Rope) HasCRLF() bool {
	return r.hasCRLF
}

// SetCRLF configures whether the file will be saved with CRLF line endings.
func (r *Rope) SetCRLF(crlf bool) {
	r.hasCRLF = crlf
}

// ByteOffsetForLine returns the 0-based byte offset where lineIdx begins in O(log N) time.
// lineIdx is 0-based (0 <= lineIdx < TotalLines()).
func (r *Rope) ByteOffsetForLine(lineIdx int) (int, error) {
	if lineIdx < 0 || lineIdx >= r.TotalLines() {
		return 0, ErrLineOutOfBounds
	}
	if lineIdx == 0 {
		return 0, nil
	}

	target := lineIdx
	curr := r.root
	offsetAccum := 0

	for curr != nil {
		if curr.isLeaf() {
			sub := curr.val
			subOffset := 0
			remaining := target
			for remaining > 0 {
				idx := bytes.IndexByte(sub, '\n')
				if idx == -1 {
					return 0, fmt.Errorf("corrupt line index: newline not found in leaf")
				}
				if remaining == 1 {
					return offsetAccum + subOffset + idx + 1, nil
				}
				remaining--
				subOffset += idx + 1
				sub = sub[idx+1:]
			}
			return 0, fmt.Errorf("corrupt line index: exhausted leaf")
		}

		leftLines := nodeLines(curr.left)
		if leftLines >= target {
			curr = curr.left
		} else {
			target -= leftLines
			offsetAccum += nodeWeight(curr.left)
			curr = curr.right
		}
	}

	return 0, ErrLineOutOfBounds
}

// LineForByteOffset returns the 0-based line index containing byteOffset in O(log N) time.
// byteOffset is 0-based (0 <= byteOffset <= TotalBytes()).
func (r *Rope) LineForByteOffset(offset int) (int, error) {
	if offset < 0 || offset > r.TotalBytes() {
		return 0, ErrOffsetOutOfBounds
	}
	if offset == 0 {
		return 0, nil
	}

	targetByte := offset
	lineAccum := 0
	curr := r.root

	for curr != nil {
		if curr.isLeaf() {
			k := targetByte
			if k > len(curr.val) {
				k = len(curr.val)
			}
			lineAccum += bytes.Count(curr.val[:k], []byte{'\n'})
			return lineAccum, nil
		}

		leftWeight := nodeWeight(curr.left)
		if targetByte < leftWeight {
			curr = curr.left
		} else {
			lineAccum += nodeLines(curr.left)
			targetByte -= leftWeight
			curr = curr.right
		}
	}

	return lineAccum, nil
}

// Insert inserts text at byte offset in O(log N) time.
func (r *Rope) Insert(offset int, text []byte) error {
	if offset < 0 || offset > r.TotalBytes() {
		return ErrOffsetOutOfBounds
	}
	if len(text) == 0 {
		return nil
	}

	// Build chunk or tree for inserted text
	var insertTree *Node
	if len(text) <= MaxChunkSize {
		leafBytes := make([]byte, len(text))
		copy(leafBytes, text)
		insertTree = newLeaf(leafBytes)
	} else {
		insertTree = BuildTreeFromBytes(text)
	}

	if r.root == nil {
		r.root = insertTree
		return nil
	}

	left, right := Split(r.root, offset)
	r.root = Concat(Concat(left, insertTree), right)
	return nil
}

// Delete removes length bytes starting at byte offset in O(log N) time.
func (r *Rope) Delete(offset, length int) error {
	if offset < 0 || offset+length > r.TotalBytes() || length < 0 {
		return ErrOffsetOutOfBounds
	}
	if length == 0 {
		return nil
	}

	left, rest := Split(r.root, offset)
	_, right := Split(rest, length)
	r.root = Concat(left, right)
	return nil
}

// ApplyEdit performs an atomic replacement of deleteLen bytes at offset with insertText.
func (r *Rope) ApplyEdit(offset int, deleteLen int, insertText string) error {
	if deleteLen > 0 {
		if err := r.Delete(offset, deleteLen); err != nil {
			return err
		}
	}
	if len(insertText) > 0 {
		if err := r.Insert(offset, []byte(insertText)); err != nil {
			return err
		}
	}
	return nil
}

// ChunkIterator traverses leaf nodes in-order without heap allocations.
type ChunkIterator struct {
	stack [64]*Node
	depth int
}

// Iterator returns an in-order chunk iterator over the rope.
func (r *Rope) Iterator() ChunkIterator {
	var it ChunkIterator
	it.pushLeft(r.root)
	return it
}

func (it *ChunkIterator) pushLeft(n *Node) {
	for n != nil {
		if it.depth >= len(it.stack) {
			break
		}
		it.stack[it.depth] = n
		it.depth++
		if n.isLeaf() {
			break
		}
		n = n.left
	}
}

// Next returns the next leaf chunk and a boolean indicating whether a chunk was found.
// The returned byte slice directly references the leaf chunk without heap allocation.
func (it *ChunkIterator) Next() ([]byte, bool) {
	if it.depth == 0 {
		return nil, false
	}

	it.depth--
	curr := it.stack[it.depth]
	val := curr.val

	for it.depth > 0 {
		parent := it.stack[it.depth-1]
		if parent.left == curr {
			it.pushLeft(parent.right)
			break
		}
		curr = parent
		it.depth--
	}

	return val, true
}

// GetLine returns the contents of lineIdx.
// If the line is contained within a single chunk, it returns a sub-slice directly (0 heap allocations).
// If the line spans chunk boundaries, it copies into an internal scratch buffer without allocating.
func (r *Rope) GetLine(lineIdx int) ([]byte, error) {
	if lineIdx < 0 || lineIdx >= r.TotalLines() {
		return nil, ErrLineOutOfBounds
	}

	startOffset, err := r.ByteOffsetForLine(lineIdx)
	if err != nil {
		return nil, err
	}

	var endOffset int
	if lineIdx+1 < r.TotalLines() {
		endOffset, err = r.ByteOffsetForLine(lineIdx + 1)
		if err != nil {
			return nil, err
		}
	} else {
		endOffset = r.TotalBytes()
	}

	lineLen := endOffset - startOffset
	if lineLen == 0 {
		return []byte{}, nil
	}

	// Try single leaf fast-path: find the leaf containing startOffset
	leaf, leafStart := r.findLeaf(startOffset)
	if leaf != nil && (startOffset+lineLen) <= (leafStart+len(leaf.val)) {
		localStart := startOffset - leafStart
		return leaf.val[localStart : localStart+lineLen], nil
	}

	// Multi-chunk boundary line: copy into scratch buffer
	if cap(r.scratch) < lineLen {
		r.scratch = make([]byte, lineLen)
	} else {
		r.scratch = r.scratch[:lineLen]
	}

	nCopied, err := r.SliceTo(startOffset, endOffset, r.scratch)
	if err != nil {
		return nil, err
	}
	return r.scratch[:nCopied], nil
}

func (r *Rope) findLeaf(offset int) (*Node, int) {
	curr := r.root
	currStart := 0
	target := offset

	for curr != nil {
		if curr.isLeaf() {
			return curr, currStart
		}
		leftWeight := nodeWeight(curr.left)
		if target < leftWeight {
			curr = curr.left
		} else {
			target -= leftWeight
			currStart += leftWeight
			curr = curr.right
		}
	}
	return nil, 0
}

// Slice returns a byte slice covering [startByte, endByte).
func (r *Rope) Slice(startByte, endByte int) ([]byte, error) {
	if startByte < 0 || endByte > r.TotalBytes() || startByte > endByte {
		return nil, ErrOffsetOutOfBounds
	}
	sliceLen := endByte - startByte
	if sliceLen == 0 {
		return []byte{}, nil
	}

	leaf, leafStart := r.findLeaf(startByte)
	if leaf != nil && endByte <= (leafStart+len(leaf.val)) {
		localStart := startByte - leafStart
		return leaf.val[localStart : localStart+sliceLen], nil
	}

	dst := make([]byte, sliceLen)
	_, err := r.SliceTo(startByte, endByte, dst)
	return dst, err
}

// SliceTo copies bytes from [startByte, endByte) into dst without heap allocation.
func (r *Rope) SliceTo(startByte, endByte int, dst []byte) (int, error) {
	if startByte < 0 || endByte > r.TotalBytes() || startByte > endByte {
		return 0, ErrOffsetOutOfBounds
	}
	needed := endByte - startByte
	if len(dst) < needed {
		return 0, io.ErrShortBuffer
	}
	if needed == 0 {
		return 0, nil
	}

	dstOffset := 0
	r.sliceRange(r.root, 0, startByte, endByte, dst, &dstOffset)
	return dstOffset, nil
}

func (r *Rope) sliceRange(n *Node, nodeOffset int, start, end int, dst []byte, dstOffset *int) {
	if n == nil || start >= end {
		return
	}

	if n.isLeaf() {
		leafEnd := nodeOffset + len(n.val)
		s := start
		if s < nodeOffset {
			s = nodeOffset
		}
		e := end
		if e > leafEnd {
			e = leafEnd
		}
		if s < e {
			srcStart := s - nodeOffset
			srcEnd := e - nodeOffset
			nCopied := copy(dst[*dstOffset:], n.val[srcStart:srcEnd])
			*dstOffset += nCopied
		}
		return
	}

	leftWeight := nodeWeight(n.left)
	leftEnd := nodeOffset + leftWeight

	if start < leftEnd {
		sEnd := end
		if sEnd > leftEnd {
			sEnd = leftEnd
		}
		r.sliceRange(n.left, nodeOffset, start, sEnd, dst, dstOffset)
	}

	if end > leftEnd {
		sStart := start
		if sStart < leftEnd {
			sStart = leftEnd
		}
		r.sliceRange(n.right, leftEnd, sStart, end, dst, dstOffset)
	}
}

// BuildTreeFromBytes builds a balanced AVL tree from raw bytes in O(N) linear time.
func BuildTreeFromBytes(data []byte) *Node {
	if len(data) == 0 {
		return nil
	}

	var leaves []*Node
	offset := 0
	for offset < len(data) {
		end := offset + MaxChunkSize
		if end > len(data) {
			end = len(data)
		} else {
			// Align split to UTF-8 rune boundary to prevent splitting code points
			for end < len(data) && (data[end]&0xC0) == 0x80 {
				end++
			}
		}

		chunk := make([]byte, end-offset)
		copy(chunk, data[offset:end])
		leaves = append(leaves, newLeaf(chunk))
		offset = end
	}

	return buildBalancedTree(leaves)
}

func buildBalancedTree(leaves []*Node) *Node {
	if len(leaves) == 0 {
		return nil
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	mid := len(leaves) / 2
	left := buildBalancedTree(leaves[:mid])
	right := buildBalancedTree(leaves[mid:])
	n := &Node{
		left:  left,
		right: right,
	}
	n.update()
	return n
}

// NormalizeCRLF strips '\r' from CRLF pairs in-place or returning a compacted slice.
// Returns the normalized slice and a boolean indicating whether CRLF was detected.
func NormalizeCRLF(src []byte) ([]byte, bool) {
	if !bytes.Contains(src, []byte("\r\n")) {
		return src, false
	}

	dst := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		if src[i] == '\r' && i+1 < len(src) && src[i+1] == '\n' {
			continue // Skip '\r'
		}
		dst = append(dst, src[i])
	}
	return dst, true
}

// Load reads a file from disk, normalizes CRLF to LF, and constructs the augmented AVL rope.
func (r *Rope) Load(filepath string) error {
	raw, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	normalized, hasCRLF := NormalizeCRLF(raw)
	r.hasCRLF = hasCRLF
	r.root = BuildTreeFromBytes(normalized)
	return nil
}

// SaveAtomic safely writes the buffer to filepath using atomic write (.tmp -> Sync -> Rename).
// If hasCRLF is true, '\n' line endings are converted back to '\r\n' on the fly.
func (r *Rope) SaveAtomic(filepath string) error {
	tmpPath := filepath + ".tahr.tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(tmpPath) // Safe cleanup if rename fails
	}()

	w := bufio.NewWriterSize(f, 64*1024)

	it := r.Iterator()
	crlfBytes := []byte("\r\n")

	for {
		chunk, ok := it.Next()
		if !ok {
			break
		}

		if !r.hasCRLF {
			if _, err := w.Write(chunk); err != nil {
				return err
			}
		} else {
			start := 0
			for i := 0; i < len(chunk); i++ {
				if chunk[i] == '\n' {
					if _, err := w.Write(chunk[start:i]); err != nil {
						return err
					}
					if _, err := w.Write(crlfBytes); err != nil {
						return err
					}
					start = i + 1
				}
			}
			if start < len(chunk) {
				if _, err := w.Write(chunk[start:]); err != nil {
					return err
				}
			}
		}
	}

	if err := w.Flush(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, filepath)
}
