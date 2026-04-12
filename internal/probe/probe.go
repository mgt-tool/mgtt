// Package probe defines the Executor interface and supporting types for
// running diagnostic probes against infrastructure components.
//
// The probe package itself only defines the interface. The caller (CLI layer)
// is responsible for constructing either a fixture.Executor or an
// exec.Executor based on the MGTT_FIXTURES environment variable:
//
//	var executor probe.Executor
//	if path := os.Getenv("MGTT_FIXTURES"); path != "" {
//	    executor, _ = fixture.Load(path)
//	} else {
//	    executor = exec.Default()
//	}
package probe
