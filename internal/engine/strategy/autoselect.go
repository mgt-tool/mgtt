package strategy

// AutoSelect picks a strategy based on whether scenarios are available.
// occam when non-empty; bfs otherwise.
func AutoSelect(in Input) Strategy {
	if len(in.Scenarios) > 0 {
		return Occam()
	}
	return BFS()
}
