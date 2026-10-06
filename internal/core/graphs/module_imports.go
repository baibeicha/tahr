package graphs

import (
	"fmt"
	"regexp"

	"tahr/internal/core/dag"
)

var (
	goImportRe   = regexp.MustCompile(`(?m)^\s*import\s+(?:\(\s*([^)]+)\s*\)|"([^"]+)")`)
	pyImportRe   = regexp.MustCompile(`(?m)^\s*(?:from\s+([a-zA-Z0-9_.]+)\s+import|import\s+([a-zA-Z0-9_.]+))`)
	tsImportRe   = regexp.MustCompile(`(?m)^\s*import\s+.*?\s+from\s+['"]([^'"]+)['"]`)
)

// ModuleDep represents a directed dependency from one module to another.
type ModuleDep struct {
	From     string `json:"from"`
	To       string `json:"to"`
	IsCyclic bool   `json:"is_cyclic"`
}

// ModuleMetrics stores architectural coupling analysis for a module.
type ModuleMetrics struct {
	Name        string  `json:"name"`
	InDegree    int     `json:"in_degree"`  // Afferent coupling (Ca)
	OutDegree   int     `json:"out_degree"` // Efferent coupling (Ce)
	Instability float64 `json:"instability"` // Ce / (Ca + Ce)
	IsGodObject bool    `json:"is_god_object"`
}

// DependencyGraph manages modules, dependencies, and cycle analysis.
type DependencyGraph struct {
	Modules []string
	Deps    []ModuleDep
	Cycles  [][]string // Groups of modules forming strongly connected components
	Metrics map[string]*ModuleMetrics
}

// NewDependencyGraph initializes an empty graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		Modules: make([]string, 0),
		Deps:    make([]ModuleDep, 0),
		Cycles:  make([][]string, 0),
		Metrics: make(map[string]*ModuleMetrics),
	}
}

// AddDependency records a dependency between two modules.
func (g *DependencyGraph) AddDependency(from, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	g.ensureModule(from)
	g.ensureModule(to)
	g.Deps = append(g.Deps, ModuleDep{From: from, To: to})
}

func (g *DependencyGraph) ensureModule(name string) {
	for _, m := range g.Modules {
		if m == name {
			return
		}
	}
	g.Modules = append(g.Modules, name)
}

// AnalyzeTarjanSCC runs Tarjan's Strongly Connected Components algorithm to detect cyclic import loops.
func (g *DependencyGraph) AnalyzeTarjanSCC() {
	adj := make(map[string][]string)
	for _, dep := range g.Deps {
		adj[dep.From] = append(adj[dep.From], dep.To)
	}

	index := 0
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string

	g.Cycles = make([][]string, 0)

	var strongConnect func(v string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, visited := indices[w]; !visited {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		// Root of SCC found
		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			// Cycles have 2 or more vertices
			if len(scc) > 1 {
				g.Cycles = append(g.Cycles, scc)
			}
		}
	}

	for _, m := range g.Modules {
		if _, visited := indices[m]; !visited {
			strongConnect(m)
		}
	}

	// An edge is cyclic if both its endpoints belong to the same SCC component of size > 1
	nodeToSCC := make(map[string]int)
	for sccID, c := range g.Cycles {
		for _, m := range c {
			nodeToSCC[m] = sccID + 1
		}
	}

	for i := range g.Deps {
		sccFrom := nodeToSCC[g.Deps[i].From]
		sccTo := nodeToSCC[g.Deps[i].To]
		if sccFrom > 0 && sccFrom == sccTo {
			g.Deps[i].IsCyclic = true
		}
	}

	// Calculate coupling metrics
	g.calculateMetrics()
}

func (g *DependencyGraph) calculateMetrics() {
	g.Metrics = make(map[string]*ModuleMetrics)
	for _, m := range g.Modules {
		g.Metrics[m] = &ModuleMetrics{Name: m}
	}
	for _, dep := range g.Deps {
		if fromM := g.Metrics[dep.From]; fromM != nil {
			fromM.OutDegree++
		}
		if toM := g.Metrics[dep.To]; toM != nil {
			toM.InDegree++
		}
	}
	for _, met := range g.Metrics {
		tot := met.InDegree + met.OutDegree
		if tot > 0 {
			met.Instability = float64(met.OutDegree) / float64(tot)
		}
		if met.InDegree > 5 && met.OutDegree > 5 {
			met.IsGodObject = true
		}
	}
}

// ToGraphModel projects the dependency graph into an interactive DAG Canvas model.
func (g *DependencyGraph) ToGraphModel() *dag.GraphModel {
	gm := dag.NewGraphModel()

	for _, m := range g.Modules {
		met := g.Metrics[m]
		badge := "Module"
		if met != nil && met.IsGodObject {
			badge = "⚠️ God-Object"
		}

		detail := "Normal"
		if met != nil {
			detail = fmt.Sprintf("In:%d Out:%d Instab:%.2f", met.InDegree, met.OutDegree, met.Instability)
		}

		card := &dag.NodeCard{
			ID:    m,
			Title: m,
			Badge: badge,
			Rows: []dag.CardRow{
				{Name: detail, DataType: "package"},
			},
			Ports: []dag.Port{
				{ID: "in", RowIndex: 0, Side: 'L', Type: dag.PortInput},
				{ID: "out", RowIndex: 0, Side: 'R', Type: dag.PortOutput},
			},
		}
		gm.AddNode(card)
	}

	for _, dep := range g.Deps {
		edge := gm.AddEdge(dep.From, "out", dep.To, "in", dag.MarkerArrow)
		if dep.IsCyclic {
			edge.Style = dag.EdgeDashed
		}
	}

	return gm
}
