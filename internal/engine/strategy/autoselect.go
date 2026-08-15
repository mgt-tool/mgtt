// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

// AutoSelect picks a strategy based on whether scenarios are available.
// occam when non-empty; bfs otherwise.
func AutoSelect(in Input) Strategy {
	if len(in.Scenarios) > 0 {
		return Occam()
	}
	return BFS()
}
