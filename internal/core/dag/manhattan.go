package dag

// Rect represents a 2D bounding box with clearance padding.
type Rect struct {
	X, Y, W, H int
}

// Contains checks if point (px, py) is strictly inside rect.
func (r Rect) Contains(px, py int) bool {
	return px >= r.X && px < r.X+r.W && py >= r.Y && py < r.Y+r.H
}

// IntersectsSegment checks if line segment from (x1, y1) to (x2, y2) intersects rect.
func (r Rect) IntersectsSegment(x1, y1, x2, y2 int) bool {
	if x1 == x2 { // Vertical segment
		minY, maxY := y1, y2
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		if x1 >= r.X && x1 < r.X+r.W {
			return !(maxY < r.Y || minY >= r.Y+r.H)
		}
	} else if y1 == y2 { // Horizontal segment
		minX, maxX := x1, x2
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if y1 >= r.Y && y1 < r.Y+r.H {
			return !(maxX < r.X || minX >= r.X+r.W)
		}
	}
	return false
}

// ChannelRouter performs fast O(V+E) orthogonal Manhattan routing with obstacle avoidance.
type ChannelRouter struct {
	edgeCache map[string][][2]int // EdgeID -> Points
}

// NewChannelRouter initializes a router with an edge waypoint cache.
func NewChannelRouter() *ChannelRouter {
	return &ChannelRouter{
		edgeCache: make(map[string][][2]int),
	}
}

// RouteAll calculates Manhattan routes for all edges in the model.
func (cr *ChannelRouter) RouteAll(model *GraphModel) {
	if model == nil {
		return
	}
	obstacles := cr.collectObstacles(model)
	for _, edge := range model.Edges {
		cr.routeEdge(model, edge, obstacles)
	}
}

// RouteIncremental recalculates ONLY the edges connected to movedNodeID.
func (cr *ChannelRouter) RouteIncremental(model *GraphModel, movedNodeID string) {
	if model == nil {
		return
	}
	obstacles := cr.collectObstacles(model)
	incident := model.IncidentEdges(movedNodeID)
	for _, edge := range incident {
		delete(cr.edgeCache, edge.ID)
		cr.routeEdge(model, edge, obstacles)
	}
}

// NodeObstacle represents an obstacle card on the canvas.
type NodeObstacle struct {
	NodeID string
	Bounds Rect
}

func (cr *ChannelRouter) collectObstacles(model *GraphModel) []NodeObstacle {
	var obstacles []NodeObstacle
	for _, n := range model.Nodes {
		// Card bounds with 1-cell padding
		obstacles = append(obstacles, NodeObstacle{
			NodeID: n.ID,
			Bounds: Rect{
				X: n.X - 1,
				Y: n.Y - 1,
				W: n.Width + 2,
				H: n.Height + 2,
			},
		})
	}
	return obstacles
}

func (cr *ChannelRouter) routeEdge(model *GraphModel, edge *Edge, obstacles []NodeObstacle) {
	srcPt, ok1 := model.GetPortCoords(edge.FromNode, edge.FromPort)
	dstPt, ok2 := model.GetPortCoords(edge.ToNode, edge.ToPort)
	if !ok1 || !ok2 {
		return
	}

	var pts [][2]int

	if srcPt[0] <= dstPt[0] {
		// Forward direction (left-to-right)
		// Check for intervening obstacles between source and destination columns
		var intervening []Rect
		for _, obs := range obstacles {
			if obs.NodeID == edge.FromNode || obs.NodeID == edge.ToNode {
				continue
			}
			if obs.Bounds.X+obs.Bounds.W > srcPt[0] && obs.Bounds.X < dstPt[0] {
				intervening = append(intervening, obs.Bounds)
			}
		}

		if len(intervening) == 0 {
			// Neighboring layers: straight line if same Y, else distributed vertical channel lane
			if srcPt[1] == dstPt[1] {
				pts = [][2]int{srcPt, dstPt}
			} else {
				dist := dstPt[0] - srcPt[0]
				midX := (srcPt[0] + dstPt[0]) / 2
				if dist >= 6 {
					lane := ((srcPt[1]*3 + dstPt[1]*7) & 0x7FFFFFFF) % 3
					candX := srcPt[0] + 2 + lane*2
					if candX < dstPt[0]-1 {
						midX = candX
					}
				}
				pts = [][2]int{
					srcPt,
					{midX, srcPt[1]},
					{midX, dstPt[1]},
					dstPt,
				}
			}
		} else {
			// Multi-layer edge crossing intermediate cards: route via highway above or below
			minY := 999999
			maxY := -999999
			for _, b := range intervening {
				if b.Y < minY {
					minY = b.Y
				}
				if b.Y+b.H > maxY {
					maxY = b.Y + b.H
				}
			}
			avgY := (srcPt[1] + dstPt[1]) / 2
			centerObsY := (minY + maxY) / 2

			var bypassY int
			if avgY <= centerObsY {
				bypassY = minY - 2
				if bypassY < 1 {
					bypassY = 1
				}
			} else {
				bypassY = maxY + 2
			}

			pts = [][2]int{
				srcPt,
				{srcPt[0] + 2, srcPt[1]},
				{srcPt[0] + 2, bypassY},
				{dstPt[0] - 2, bypassY},
				{dstPt[0] - 2, dstPt[1]},
				dstPt,
			}
		}
	} else {
		// Backward / cycle edge (right-to-left): route via highway around cards
		minY := 999999
		maxY := -999999
		for _, obs := range obstacles {
			if obs.NodeID == edge.FromNode || obs.NodeID == edge.ToNode {
				continue
			}
			if obs.Bounds.X+obs.Bounds.W > dstPt[0] && obs.Bounds.X < srcPt[0] {
				if obs.Bounds.Y < minY {
					minY = obs.Bounds.Y
				}
				if obs.Bounds.Y+obs.Bounds.H > maxY {
					maxY = obs.Bounds.Y + obs.Bounds.H
				}
			}
		}
		if minY == 999999 {
			minY = srcPt[1]
			if dstPt[1] < minY {
				minY = dstPt[1]
			}
			maxY = srcPt[1]
			if dstPt[1] > maxY {
				maxY = dstPt[1]
			}
		}

		avgY := (srcPt[1] + dstPt[1]) / 2
		centerObsY := (minY + maxY) / 2

		var bypassY int
		if avgY <= centerObsY {
			bypassY = minY - 2
			if bypassY < 1 {
				bypassY = 1
			}
		} else {
			bypassY = maxY + 2
		}

		pts = [][2]int{
			srcPt,
			{srcPt[0] + 2, srcPt[1]},
			{srcPt[0] + 2, bypassY},
			{dstPt[0] - 2, bypassY},
			{dstPt[0] - 2, dstPt[1]},
			dstPt,
		}
	}

	pts = simplifyPoints(pts)
	edge.Points = pts
	cr.edgeCache[edge.ID] = pts
}

func simplifyPoints(pts [][2]int) [][2]int {
	if len(pts) <= 2 {
		return pts
	}
	res := make([][2]int, 0, len(pts))
	res = append(res, pts[0])
	for i := 1; i < len(pts)-1; i++ {
		prev := res[len(res)-1]
		curr := pts[i]
		next := pts[i+1]
		if curr == prev {
			continue
		}
		if prev[1] == curr[1] && curr[1] == next[1] {
			continue
		}
		if prev[0] == curr[0] && curr[0] == next[0] {
			continue
		}
		res = append(res, curr)
	}
	last := pts[len(pts)-1]
	if last != res[len(res)-1] {
		res = append(res, last)
	}
	return res
}
