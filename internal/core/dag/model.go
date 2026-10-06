package dag

import (
	"fmt"
)

// PortType defines signal/link direction on an anchor port.
type PortType byte

const (
	PortInput  PortType = 0x01
	PortOutput PortType = 0x02
	PortBoth   PortType = 0x03
)

// Port represents an anchor point on the left ('L') or right ('R') boundary of a card row.
type Port struct {
	ID       string   `json:"id"`
	RowIndex int      `json:"row_index"` // 0-based index of row in card
	Side     byte     `json:"side"`      // 'L' or 'R'
	Type     PortType `json:"type"`
}

// CardRow represents a single attribute/column row inside a table or entity card.
type CardRow struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	IsPK       bool   `json:"is_pk"`
	IsFK       bool   `json:"is_fk"`
	IsNullable bool   `json:"is_nullable"`
	IsIndex    bool   `json:"is_index"`
}

// NodeCard represents a rectangular entity card in the DAG Canvas.
type NodeCard struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Badge  string    `json:"badge,omitempty"` // e.g. "Table", "Trait", "Service"
	Rows   []CardRow `json:"rows"`
	Ports  []Port    `json:"ports"`
	X      int       `json:"x"`
	Y      int       `json:"y"`
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Layer  int       `json:"layer"` // Sugiyama rank/layer
}

// EdgeStyle defines line rendering attributes.
type EdgeStyle byte

const (
	EdgeSolid  EdgeStyle = 0x01
	EdgeDashed EdgeStyle = 0x02
	EdgeDotted EdgeStyle = 0x03
)

// Edge markers
const (
	MarkerNone          = "none"
	MarkerArrow         = "arrow"
	MarkerCrowFootOne   = "1:1"
	MarkerCrowFootMany  = "1:N"
)

// Edge represents an orthogonal directed link between two ports.
type Edge struct {
	ID          string    `json:"id"`
	FromNode    string    `json:"from_node"`
	FromPort    string    `json:"from_port"`
	ToNode      string    `json:"to_node"`
	ToPort      string    `json:"to_port"`
	Style       EdgeStyle `json:"style"`
	MarkerStart string    `json:"marker_start"`
	MarkerEnd   string    `json:"marker_end"`
	Points      [][2]int  `json:"points"` // List of [x, y] Manhattan vertices
}

// GraphModel is the universal intermediate representation (Graph IR) for DAG Canvas.
type GraphModel struct {
	Nodes map[string]*NodeCard `json:"nodes"`
	Edges []*Edge              `json:"edges"`
}

// NewGraphModel initializes an empty graph model.
func NewGraphModel() *GraphModel {
	return &GraphModel{
		Nodes: make(map[string]*NodeCard),
		Edges: make([]*Edge, 0),
	}
}

// AddNode adds or updates a node card in the model, calculating its minimum width and height.
func (gm *GraphModel) AddNode(card *NodeCard) {
	if card == nil {
		return
	}
	// Compute default dimensions based on title and rows
	minW := len(card.Title) + 6
	for _, r := range card.Rows {
		rowW := len(r.Name) + len(r.DataType) + 10
		if rowW > minW {
			minW = rowW
		}
	}
	if card.Width < minW {
		card.Width = minW
	}
	minH := len(card.Rows) + 3 // Title + divider + rows + bottom border
	if card.Height < minH {
		card.Height = minH
	}
	gm.Nodes[card.ID] = card
}

// AddEdge creates and registers an edge between nodes.
func (gm *GraphModel) AddEdge(fromNode, fromPort, toNode, toPort string, markerEnd string) *Edge {
	edgeID := fmt.Sprintf("%s:%s->%s:%s", fromNode, fromPort, toNode, toPort)
	edge := &Edge{
		ID:          edgeID,
		FromNode:    fromNode,
		FromPort:    fromPort,
		ToNode:      toNode,
		ToPort:      toPort,
		Style:       EdgeSolid,
		MarkerStart: MarkerNone,
		MarkerEnd:   markerEnd,
		Points:      make([][2]int, 0),
	}
	gm.Edges = append(gm.Edges, edge)
	return edge
}

// IncidentEdges returns all edges connected to a given node ID.
func (gm *GraphModel) IncidentEdges(nodeID string) []*Edge {
	var connected []*Edge
	for _, e := range gm.Edges {
		if e.FromNode == nodeID || e.ToNode == nodeID {
			connected = append(connected, e)
		}
	}
	return connected
}

// GetPortCoords calculates the global (X, Y) canvas coordinates of a port on a node card.
func (gm *GraphModel) GetPortCoords(nodeID, portID string) ([2]int, bool) {
	node, exists := gm.Nodes[nodeID]
	if !exists {
		return [2]int{}, false
	}
	for _, p := range node.Ports {
		if p.ID == portID {
			py := node.Y + 2 + p.RowIndex // 2 header rows
			px := node.X
			if p.Side == 'R' {
				px = node.X + node.Width - 1
			}
			return [2]int{px, py}, true
		}
	}
	// Fallback to center borders if port not found
	return [2]int{node.X + node.Width/2, node.Y + node.Height/2}, true
}
