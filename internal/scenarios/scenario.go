// Package scenarios defines the data model for failure scenarios
// enumerated from the causation graph declared in providers'
// failure_modes + triggered_by.
package scenarios

// Scenario is a complete failure-chain hypothesis.
type Scenario struct {
	ID    string
	Root  RootRef
	Chain []Step
}

type RootRef struct {
	Component string
	State     string
}

// Step is one link in the chain. Non-terminal steps carry EmitsOnEdge;
// terminal steps carry Observes.
type Step struct {
	Component   string
	State       string
	EmitsOnEdge string
	Observes    []string
}

func (s Step) IsTerminal() bool    { return len(s.Observes) > 0 }
func (s Step) IsNonTerminal() bool { return s.EmitsOnEdge != "" }

func (s Scenario) Length() int { return len(s.Chain) }

func (s Scenario) TouchesComponent(name string) bool {
	for _, step := range s.Chain {
		if step.Component == name {
			return true
		}
	}
	return false
}
