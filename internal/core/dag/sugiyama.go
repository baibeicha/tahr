package dag

import (
	"sort"
)

// SugiyamaConfig defines layout metrics and padding.
type SugiyamaConfig struct {
	HorizontalSpacing int
	VerticalSpacing   int
	StartX            int
	StartY            int
}

// DefaultSugiyamaConfig returns balanced layout spacing.
func DefaultSugiyamaConfig() SugiyamaConfig {
	return SugiyamaConfig{
		HorizontalSpacing: 12,
		VerticalSpacing:   4,
		StartX:            4,
		StartY:            2,
	}
}

// LayoutSugiyama computes 2D coordinates for all node cards in the model.
func LayoutSugiyama(model *GraphModel, cfg SugiyamaConfig) {
	if model == nil || len(model.Nodes) == 0 {
		return
	}

	// 1. Assign Layers (Longest Path layering)
	inDegree := make(map[string]int)
	outEdges := make(map[string][]string)

	for id := range model.Nodes {
		inDegree[id] = 0
		outEdges[id] = make([]string, 0)
	}

	for _, e := range model.Edges {
		inDegree[e.ToNode]++
		outEdges[e.FromNode] = append(outEdges[e.FromNode], e.ToNode)
	}

	layers := make(map[int][]*NodeCard)
	maxLayer := 0

	// Find roots (inDegree == 0)
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
			model.Nodes[id].Layer = 0
		}
	}

	// If cyclic or no pure roots, pick first node as root
	if len(queue) == 0 {
		for id := range model.Nodes {
			queue = append(queue, id)
			model.Nodes[id].Layer = 0
			break
		}
	}

	// Longest Path propagation
	visited := make(map[string]bool)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visited[curr] = true

		currLayer := model.Nodes[curr].Layer
		if currLayer > maxLayer {
			maxLayer = currLayer
		}

		for _, target := range outEdges[curr] {
			targetNode := model.Nodes[target]
			if targetNode != nil {
				if targetNode.Layer < currLayer+1 {
					targetNode.Layer = currLayer + 1
				}
				if !visited[target] {
					queue = append(queue, target)
				}
			}
		}
	}

	// Handle disconnected islands
	for id, node := range model.Nodes {
		if !visited[id] {
			node.Layer = 0
		}
		layers[node.Layer] = append(layers[node.Layer], node)
	}

	// 2. Crossing Reduction (Barycenter Ordering with Rank Indices)
	nodeRank := make(map[string]float64)
	for i, node := range layers[0] {
		nodeRank[node.ID] = float64(i)
	}

	for l := 1; l <= maxLayer; l++ {
		currLayerNodes := layers[l]
		sort.Slice(currLayerNodes, func(i, j int) bool {
			posI := barycenterRank(currLayerNodes[i].ID, model, nodeRank)
			posJ := barycenterRank(currLayerNodes[j].ID, model, nodeRank)
			if posI == posJ {
				return currLayerNodes[i].ID < currLayerNodes[j].ID
			}
			return posI < posJ
		})
		for r, node := range currLayerNodes {
			nodeRank[node.ID] = float64(r)
		}
	}

	// 3. Coordinate Assignment
	currX := cfg.StartX
	for l := 0; l <= maxLayer; l++ {
		layerNodes := layers[l]
		if len(layerNodes) == 0 {
			continue
		}

		// Determine max width of cards in this layer
		layerMaxW := 0
		for _, n := range layerNodes {
			if n.Width > layerMaxW {
				layerMaxW = n.Width
			}
		}

		// Arrange nodes vertically with verticalSpacing
		currY := cfg.StartY
		for _, n := range layerNodes {
			n.X = currX
			n.Y = currY
			currY += n.Height + cfg.VerticalSpacing
		}

		currX += layerMaxW + cfg.HorizontalSpacing
	}
}

// barycenterRank computes the average layer position of connected neighbors.
func barycenterRank(nodeID string, model *GraphModel, ranks map[string]float64) float64 {
	sumRank := 0.0
	count := 0
	for _, e := range model.Edges {
		if e.ToNode == nodeID {
			if r, ok := ranks[e.FromNode]; ok {
				sumRank += r
				count++
			}
		} else if e.FromNode == nodeID {
			if r, ok := ranks[e.ToNode]; ok {
				sumRank += r
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return sumRank / float64(count)
}
