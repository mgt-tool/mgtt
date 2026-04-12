package probe

import "context"

// Runner is a typed probe backend that extracts facts directly from
// infrastructure APIs (e.g. parsing kubectl JSON) instead of relying on
// opaque shell commands. When a Runner can handle a (componentType, fact)
// pair, it is preferred over the shell-based Executor.
type Runner interface {
	// Probe extracts a single fact for the named component.
	// vars carries context like "namespace" and "type".
	Probe(ctx context.Context, component, fact string, vars map[string]string) (Result, error)

	// CanProbe reports whether this runner supports the given
	// component type and fact combination.
	CanProbe(componentType, fact string) bool
}
