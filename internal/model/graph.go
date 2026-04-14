package model

// depGraph is an adjacency-list directed graph built from Component
// dependencies.  Edges run from dependent → dependency, matching the
// "depends on" direction (nginx → frontend means nginx needs frontend).
type depGraph struct {
	adj     map[string][]string // name → list of names this node depends on
	inDeg   map[string]int      // in-degree: how many nodes depend on this one
	order   []string            // declaration order (preserved from YAML)
}

// NewDepGraph constructs a depGraph from the component map and declaration
// order.  Components with no declared dependencies are still included.
func NewDepGraph(components map[string]*Component, order []string) *depGraph {
	g := &depGraph{
		adj:   make(map[string][]string, len(components)),
		inDeg: make(map[string]int, len(components)),
		order: order,
	}

	// Initialise every component so that nodes with no deps are still present.
	for name := range components {
		if _, ok := g.adj[name]; !ok {
			g.adj[name] = nil
		}
		if _, ok := g.inDeg[name]; !ok {
			g.inDeg[name] = 0
		}
	}

	// Add edges.
	for name, comp := range components {
		for _, dep := range comp.Depends {
			for _, target := range dep.On {
				g.adj[name] = append(g.adj[name], target)
				// target's in-degree increases because name depends on it.
				g.inDeg[target]++
			}
		}
	}

	return g
}

// EntryPoint returns the name of the first component (in declaration order)
// that has in-degree 0 — i.e., no other component depends on it. This is
// the "top of the call stack" or the public-facing entry point.
// Returns "" if no such component exists (unlikely in a valid model).
func (g *depGraph) EntryPoint() string {
	for _, name := range g.order {
		if g.inDeg[name] == 0 {
			return name
		}
	}
	return ""
}

// DependenciesOf returns the adjacency list for the named component.
func (g *depGraph) DependenciesOf(name string) []string {
	return g.adj[name]
}

// DetectCycle performs a DFS with white/gray/black colouring and returns a
// representative cycle path if one exists, or nil if the graph is acyclic.
//
// Colours:
//
//	0 = white (unvisited)
//	1 = gray  (on the current DFS stack)
//	2 = black (fully processed)
func (g *depGraph) DetectCycle() []string {
	color := make(map[string]int, len(g.adj))
	parent := make(map[string]string, len(g.adj))

	var cycle []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = 1 // gray
		for _, neighbour := range g.adj[node] {
			switch color[neighbour] {
			case 1: // back-edge → cycle found
				// Reconstruct cycle path from neighbour back through parents to
				// neighbour again.
				cycle = reconstructCycle(parent, node, neighbour)
				return true
			case 0: // white → recurse
				parent[neighbour] = node
				if dfs(neighbour) {
					return true
				}
			}
			// black → already processed, skip
		}
		color[node] = 2 // black
		return false
	}

	for _, name := range g.order {
		if color[name] == 0 {
			if dfs(name) {
				return cycle
			}
		}
	}
	return nil
}

// reconstructCycle builds the cycle path given the DFS parent map, the node
// that discovered the back-edge (from), and the target of the back-edge
// (cycleStart, which is already on the stack).
func reconstructCycle(parent map[string]string, from, cycleStart string) []string {
	path := []string{cycleStart}
	cur := from
	for cur != cycleStart {
		path = append(path, cur)
		p, ok := parent[cur]
		if !ok {
			break
		}
		cur = p
	}
	path = append(path, cycleStart)
	// Reverse so it reads start → … → start.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}
