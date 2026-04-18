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

// minimalOK is a v1.0 manifest that passes both parser validation and
// Static checks. Source install is declared so the parser accepts it;
// runtime.entrypoint is omitted so the convention ($MGTT_PROVIDER_DIR/bin/...)
// applies and no disk-existence check runs.
const minimalOK = `
meta:
  name: x
  version: 1.0.0
  description: "test provider"
install:
  source:
    build: install.sh
    clean: uninstall.sh
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

func TestStatic_DefaultsToReadOnly(t *testing.T) {
	// Absent read_only means read-only. No warnings, no failures about writes.
	r := Static(loadYAML(t, minimalOK))
	if !r.OK() {
		t.Fatalf("happy path should pass: %+v", r)
	}
	for _, wr := range r.Warnings {
		if strings.Contains(wr, "read_only") || strings.Contains(wr, "writes") {
			t.Fatalf("absent read_only should not warn; got %q", wr)
		}
	}
}

func TestStatic_FailsReadOnlyFalseWithoutWritesNote(t *testing.T) {
	// read_only: false without writes_note is rejected at parse time in v1.0;
	// LoadFromBytes returns an error. Validate that behaviour here (equivalent
	// consumer-visible guarantee).
	src := strings.Replace(minimalOK,
		"description: \"test provider\"",
		"description: \"test provider\"\nread_only: false", 1)
	if _, err := providersupport.LoadFromBytes([]byte(src)); err == nil {
		t.Fatal("read_only: false without writes_note must be rejected at parse time")
	} else if !strings.Contains(err.Error(), "writes_note") {
		t.Fatalf("error must mention writes_note; got %v", err)
	}
}

func TestStatic_WarnsReadOnlyFalseWithWritesNote(t *testing.T) {
	src := strings.Replace(minimalOK,
		"description: \"test provider\"",
		"description: \"test provider\"\nread_only: false\nwrites_note: \"touches state file on plan\"", 1)
	r := Static(loadYAML(t, src))
	if !r.OK() {
		t.Fatalf("read_only: false with writes_note should warn, not fail: %+v", r)
	}
	if !containsAny(r.Warnings, "read_only: false") {
		t.Fatalf("expected warning about non-read-only posture: %+v", r.Warnings)
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

func TestStatic_FailsOnMissingEntrypointBinary(t *testing.T) {
	// runtime.entrypoint as an absolute path gets checked for existence.
	src := strings.Replace(minimalOK,
		"install:",
		"runtime:\n  entrypoint: /nonexistent/path/xyz\ninstall:",
		1)
	r := Static(loadYAML(t, src))
	if r.OK() {
		t.Fatal("absolute entrypoint path pointing nowhere should fail")
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
	p.Runtime.Needs = map[string]string{"kubectl": "", "vault-nope": ""}
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

func TestStatic_AcceptsKnownCaps(t *testing.T) {
	p := loadYAML(t, minimalOK)
	p.Runtime.Needs = map[string]string{"kubectl": "", "aws": ""}
	r := Static(p)
	for _, f := range r.Failures {
		if strings.Contains(f, "needs") || strings.Contains(f, "capability") {
			t.Errorf("known caps must not produce failures; got %v", r.Failures)
		}
	}
}

func TestStatic_AcceptsValidNetworkModes(t *testing.T) {
	for _, mode := range []string{"", "bridge", "host"} {
		p := loadYAML(t, minimalOK)
		p.Runtime.NetworkMode = mode
		r := Static(p)
		for _, f := range r.Failures {
			if strings.Contains(f, "network") {
				t.Errorf("mode %q must be accepted; got failure %q", mode, f)
			}
		}
	}
}

func TestStatic_RejectsUnknownNetworkModeAtParseTime(t *testing.T) {
	// v1.0: network_mode enum is enforced at parse time. Validate the
	// parser rejects overlay (or any non-{"", bridge, host} value).
	src := strings.Replace(minimalOK,
		"install:",
		"runtime:\n  network_mode: overlay\ninstall:",
		1)
	if _, err := providersupport.LoadFromBytes([]byte(src)); err == nil {
		t.Fatal("unknown network_mode must be rejected at parse time")
	} else if !strings.Contains(err.Error(), "network_mode") {
		t.Fatalf("error must name the field; got %v", err)
	}
}
