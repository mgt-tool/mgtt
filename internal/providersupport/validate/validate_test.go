package validate

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

func loadYAML(t *testing.T, src string) *providersupport.Provider {
	t.Helper()
	p, err := providersupport.LoadFromBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const minimalOK = `
meta:
  name: x
  version: 1.0.0
  # Non-absolute path — the disk-existence check skips this form because
  # it's an install-time template. Absolute paths get checked.
  command: "bin/x"
auth:
  strategy: environment
  access:
    probes: read
    writes: none
types:
  thing:
    facts:
      f:
        type: mgtt.int
        probe:
          cmd: "echo 1"
          parse: int
    healthy:
      - "f > 0"
    states:
      live:
        when: "f > 0"
    default_active_state: live
`

func TestStatic_HappyPath(t *testing.T) {
	r := Static(loadYAML(t, minimalOK))
	if !r.OK() {
		t.Fatalf("happy path should pass: %+v", r)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("happy path should have no warnings: %+v", r.Warnings)
	}
}

func TestStatic_FailsWhenWritesAbsent(t *testing.T) {
	src := strings.Replace(minimalOK, "writes: none", "", 1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("missing writes should fail")
	}
	if !containsAny(r.Failures, "writes is not declared") {
		t.Fatalf("failure should explain writes: %+v", r.Failures)
	}
}

func TestStatic_WarnsWhenWritesNotNone(t *testing.T) {
	src := strings.Replace(minimalOK, "writes: none", "writes: limited", 1)
	r := Static(loadYAML(t, src))
	if !r.OK() {
		t.Fatalf("non-none writes should warn, not fail: %+v", r)
	}
	if !containsAny(r.Warnings, "writes=") {
		t.Fatalf("expected warning about writes: %+v", r.Warnings)
	}
}

func TestStatic_FailsOnDefaultActiveStateMismatch(t *testing.T) {
	src := strings.Replace(minimalOK, "default_active_state: live", "default_active_state: bogus", 1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("bogus default_active_state should fail")
	}
}

func TestStatic_FailsOnIncompatibleMgttRequires(t *testing.T) {
	src := strings.Replace(minimalOK,
		"name: x",
		"name: x\n  requires:\n    mgtt: \">=99.0.0\"",
		1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("incompatible requires should fail")
	}
	if !containsAny(r.Failures, "requires mgtt") {
		t.Fatalf("failure should explain mgtt mismatch: %+v", r.Failures)
	}
}

func TestStatic_WarnsWhenParseEmpty(t *testing.T) {
	src := strings.Replace(minimalOK, "          parse: int", "", 1)
	r := Static(loadYAML(t, src))
	if !containsAny(r.Warnings, "probe.parse empty") {
		t.Fatalf("expected parse warning: %+v", r.Warnings)
	}
}

func TestStatic_FailsOnMissingCommandBinary(t *testing.T) {
	src := strings.Replace(minimalOK, `command: "bin/x"`, `command: "/nonexistent/path/xyz"`, 1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("absolute command path pointing nowhere should fail")
	}
	if !containsAny(r.Failures, "does not exist on disk") {
		t.Fatalf("failure should mention missing binary: %+v", r.Failures)
	}
}

func TestStatic_FailsOnUndeclaredFactInHealthy(t *testing.T) {
	src := strings.Replace(minimalOK, `- "f > 0"`, `- "ghost_fact > 0"`, 1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("healthy referencing undeclared fact should fail")
	}
	if !containsAny(r.Failures, "ghost_fact") {
		t.Fatalf("failure should name the undeclared fact: %+v", r.Failures)
	}
}

func TestStatic_FailsOnUndeclaredFactInStateWhen(t *testing.T) {
	src := strings.Replace(minimalOK, `when: "f > 0"`, `when: "phantom > 0"`, 1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("state.when referencing undeclared fact should fail")
	}
	if !containsAny(r.Failures, "phantom") {
		t.Fatalf("failure should name the undeclared fact: %+v", r.Failures)
	}
}

func containsAny(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

func TestStatic_RejectsUnknownCap(t *testing.T) {
	p := loadYAML(t, minimalOK)
	p.Needs = []string{"kubectl", "vault-nope"}
	r := Static(p)
	if r.OK() {
		t.Fatal("expected validation failure for unknown cap")
	}
	found := false
	for _, f := range r.Failures {
		if strings.Contains(f, "vault-nope") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("failure must name the unknown cap; got %v", r.Failures)
	}
}

func TestStatic_RejectsNeedsOnShellFallback(t *testing.T) {
	p := loadYAML(t, minimalOK)
	p.Meta.Command = "" // shell-fallback
	p.Needs = []string{"kubectl"}
	r := Static(p)
	if r.OK() {
		t.Fatal("shell-fallback providers must not declare needs")
	}
	found := false
	for _, f := range r.Failures {
		if strings.Contains(f, "needs") && strings.Contains(f, "no command") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("failure must explain shell-fallback/no-command; got %v", r.Failures)
	}
}

func TestStatic_AcceptsKnownCaps(t *testing.T) {
	p := loadYAML(t, minimalOK)
	p.Needs = []string{"kubectl", "aws"}
	r := Static(p)
	for _, f := range r.Failures {
		if strings.Contains(f, "needs") || strings.Contains(f, "capability") {
			t.Errorf("known caps must not produce failures; got %v", r.Failures)
		}
	}
}

func TestStatic_AcceptsValidNetworkModes(t *testing.T) {
	for _, mode := range []string{"", "bridge", "host", "none"} {
		p := loadYAML(t, minimalOK)
		p.Network = mode
		r := Static(p)
		for _, f := range r.Failures {
			if strings.Contains(f, "network") {
				t.Errorf("mode %q must be accepted; got failure %q", mode, f)
			}
		}
	}
}

func TestStatic_RejectsUnknownNetworkMode(t *testing.T) {
	p := loadYAML(t, minimalOK)
	p.Network = "overlay"
	r := Static(p)
	if r.OK() {
		t.Fatal("unknown network mode must fail validation")
	}
	found := false
	for _, f := range r.Failures {
		if strings.Contains(f, "network") && strings.Contains(f, "overlay") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("failure must name the bad mode and the field; got %v", r.Failures)
	}
}
