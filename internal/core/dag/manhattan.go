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

	// 1. Initial 3-segment orthogonal channel: (src) -> (midX, srcY) -> (midX, dstY) -> (dst)
	midX := (srcPt[0] + dstPt[0]) / 2
	if srcPt[0] >= dstPt[0] {
		// Source is to the right of destination: route with clearance offset
		midX = srcPt[0] + 3
	}

	pts := [][2]int{
		srcPt,
		{midX, srcPt[1]},
		{midX, dstPt[1]},
		dstPt,
	}

	// 2. Obstacle collision detection & channel diversion
	hasCollision := false
	var blockedObstacle Rect
	for _, obs := range obstacles {
		if obs.NodeID == edge.FromNode || obs.NodeID == edge.ToNode {
			continue // Skip endpoint cards themselves
		}
		b := obs.Bounds
		if b.IntersectsSegment(srcPt[0], srcPt[1], midX, srcPt[1]) ||
			b.IntersectsSegment(midX, srcPt[1], midX, dstPt[1]) ||
			b.IntersectsSegment(midX, dstPt[1], dstPt[0], dstPt[1]) {
			hasCollision = true
			blockedObstacle = b
			break
		}
	}

	if hasCollision {
		// Divert channel above or below the blocked obstacle
		bypassY := blockedObstacle.Y - 2
		if bypassY < 0 {
			bypassY = blockedObstacle.Y + blockedObstacle.H + 2
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

	edge.Points = pts
	cr.edgeCache[edge.ID] = pts
}
