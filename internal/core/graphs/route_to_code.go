package graphs

import (
	"fmt"
	"regexp"
	"strings"

	"tahr/internal/core/dag"
)

var (
	// Detects common router patterns: r.GET("/path", handler), app.post('/path', handler), etc.
	routeCallRe = regexp.MustCompile(`(?i)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*["']([^"']+)["']\s*,\s*([a-zA-Z0-9_.]+)`)
)

// APIRoute represents an exposed HTTP endpoint and its mapped handler symbol.
type APIRoute struct {
	Method      string   `json:"method"` // GET, POST, DELETE, etc.
	Path        string   `json:"path"`   // /api/v1/orders
	HandlerName string   `json:"handler"`
	FileURI     string   `json:"file_uri"`
	Line        int      `json:"line"`
	SubCalls    []string `json:"sub_calls,omitempty"` // Services, DB queries invoked by handler
}

// GenerateHTTPRequest creates a sample RFC 7230 HTTP request string.
func (r *APIRoute) GenerateHTTPRequest(baseURL string) string {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s%s HTTP/1.1\n", r.Method, baseURL, r.Path))
	sb.WriteString("Host: localhost:8080\n")
	sb.WriteString("Content-Type: application/json\n")
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		sb.WriteString("\n{\n  \"example\": true\n}\n")
	}
	return sb.String()
}

// RoutePipelineScanner scans code files for HTTP route registrations.
type RoutePipelineScanner struct {
	Routes []*APIRoute
}

// NewRoutePipelineScanner initializes an empty route scanner.
func NewRoutePipelineScanner() *RoutePipelineScanner {
	return &RoutePipelineScanner{
		Routes: make([]*APIRoute, 0),
	}
}

// ScanFileContent parses code content for registered endpoints.
func (s *RoutePipelineScanner) ScanFileContent(fileURI, content string) {
	lines := strings.Split(content, "\n")
	for lineIdx, line := range lines {
		matches := routeCallRe.FindStringSubmatch(line)
		if len(matches) >= 4 {
			route := &APIRoute{
				Method:      strings.ToUpper(matches[1]),
				Path:        matches[2],
				HandlerName: matches[3],
				FileURI:     fileURI,
				Line:        lineIdx + 1,
			}
			s.Routes = append(s.Routes, route)
		}
	}
}

// ToGraphModel constructs an end-to-end pipeline graph connecting routes to handlers and services.
func (s *RoutePipelineScanner) ToGraphModel() *dag.GraphModel {
	gm := dag.NewGraphModel()

	for _, r := range s.Routes {
		routeNodeID := fmt.Sprintf("route:%s:%s", r.Method, r.Path)
		handlerNodeID := fmt.Sprintf("handler:%s", r.HandlerName)

		// 1. Route Node (with HTTP method badge)
		routeCard := &dag.NodeCard{
			ID:    routeNodeID,
			Title: r.Path,
			Badge: r.Method,
			Rows: []dag.CardRow{
				{Name: fmt.Sprintf("%s %s", r.Method, r.Path), DataType: "route"},
			},
			Ports: []dag.Port{
				{ID: "out", RowIndex: 0, Side: 'R', Type: dag.PortOutput},
			},
		}
		gm.AddNode(routeCard)

		// 2. Handler Node
		handlerCard := &dag.NodeCard{
			ID:    handlerNodeID,
			Title: r.HandlerName,
			Badge: "Handler",
			Rows: []dag.CardRow{
				{Name: fmt.Sprintf("line %d", r.Line), DataType: "func"},
			},
			Ports: []dag.Port{
				{ID: "in", RowIndex: 0, Side: 'L', Type: dag.PortInput},
				{ID: "out", RowIndex: 0, Side: 'R', Type: dag.PortOutput},
			},
		}
		gm.AddNode(handlerCard)

		// Connect Route -> Handler
		gm.AddEdge(routeNodeID, "out", handlerNodeID, "in", dag.MarkerArrow)

		// 3. Connect Handler -> SubCalls (Services / DB)
		for _, sub := range r.SubCalls {
			subID := fmt.Sprintf("svc:%s", sub)
			subCard := &dag.NodeCard{
				ID:    subID,
				Title: sub,
				Badge: "Service/DB",
				Rows: []dag.CardRow{
					{Name: sub, DataType: "backend"},
				},
				Ports: []dag.Port{
					{ID: "in", RowIndex: 0, Side: 'L', Type: dag.PortInput},
				},
			}
			gm.AddNode(subCard)
			gm.AddEdge(handlerNodeID, "out", subID, "in", dag.MarkerArrow)
		}
	}

	return gm
}
