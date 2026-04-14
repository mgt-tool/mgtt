package probe_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe/fixture"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("assertEqual: got %v (%T), want %v (%T)", got, got, want, want)
	}
}

func assertAlmostEqual(t *testing.T, got any, want float64) {
	t.Helper()
	f, ok := got.(float64)
	if !ok {
		t.Fatalf("assertAlmostEqual: got %T, want float64", got)
	}
	if math.Abs(f-want) > 1e-9 {
		t.Fatalf("assertAlmostEqual: got %v, want %v", f, want)
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — int
// ---------------------------------------------------------------------------

func TestParseOutput_Int(t *testing.T) {
	v, err := probe.ParseOutput("int", "  42\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 42)
}

func TestParseOutput_Int_Negative(t *testing.T) {
	v, err := probe.ParseOutput("int", "-7\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, -7)
}

func TestParseOutput_Int_Error(t *testing.T) {
	_, err := probe.ParseOutput("int", "not-a-number", 0)
	if err == nil {
		t.Fatal("expected error for non-numeric int input")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — float
// ---------------------------------------------------------------------------

func TestParseOutput_Float(t *testing.T) {
	v, err := probe.ParseOutput("float", "3.14\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertAlmostEqual(t, v, 3.14)
}

func TestParseOutput_Float_WholeNumber(t *testing.T) {
	v, err := probe.ParseOutput("float", "100\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertAlmostEqual(t, v, 100.0)
}

func TestParseOutput_Float_Error(t *testing.T) {
	_, err := probe.ParseOutput("float", "not-a-float", 0)
	if err == nil {
		t.Fatal("expected error for non-numeric float input")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — bool
// ---------------------------------------------------------------------------

func TestParseOutput_Bool_True(t *testing.T) {
	v, err := probe.ParseOutput("bool", "true\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, true)
}

func TestParseOutput_Bool_Yes(t *testing.T) {
	v, err := probe.ParseOutput("bool", "YES\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, true)
}

func TestParseOutput_Bool_One(t *testing.T) {
	v, err := probe.ParseOutput("bool", "1\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, true)
}

func TestParseOutput_Bool_False(t *testing.T) {
	v, err := probe.ParseOutput("bool", "false\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, false)
}

func TestParseOutput_Bool_No(t *testing.T) {
	v, err := probe.ParseOutput("bool", "no\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, false)
}

func TestParseOutput_Bool_Zero(t *testing.T) {
	v, err := probe.ParseOutput("bool", "0\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, false)
}

func TestParseOutput_Bool_Error(t *testing.T) {
	_, err := probe.ParseOutput("bool", "maybe\n", 0)
	if err == nil {
		t.Fatal("expected error for unrecognised bool value")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — string
// ---------------------------------------------------------------------------

func TestParseOutput_String(t *testing.T) {
	v, err := probe.ParseOutput("string", "  hello world  \n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "hello world")
}

func TestParseOutput_String_Empty(t *testing.T) {
	v, err := probe.ParseOutput("string", "\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "")
}

// ---------------------------------------------------------------------------
// ParseOutput — exit_code
// ---------------------------------------------------------------------------

func TestParseOutput_ExitCode_Zero(t *testing.T) {
	v, err := probe.ParseOutput("exit_code", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, true)
}

func TestParseOutput_ExitCode_NonZero(t *testing.T) {
	v, err := probe.ParseOutput("exit_code", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, false)
}

func TestParseOutput_ExitCode_IgnoresStdout(t *testing.T) {
	v, err := probe.ParseOutput("exit_code", "some output\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, true)
}

// ---------------------------------------------------------------------------
// ParseOutput — regex
// ---------------------------------------------------------------------------

func TestParseOutput_Regex(t *testing.T) {
	v, err := probe.ParseOutput(`regex:(\d+)`, "restart count: 47\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "47")
}

func TestParseOutput_Regex_NoGroup(t *testing.T) {
	v, err := probe.ParseOutput(`regex:available`, "available\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "available")
}

func TestParseOutput_Regex_BoolPattern(t *testing.T) {
	// The aws rds probe uses regex:^available$ to match "available\n".
	v, err := probe.ParseOutput(`regex:^available$`, "available\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	// No capture group → returns whole match.
	assertEqual(t, v, "available")
}

func TestParseOutput_Regex_NoMatch(t *testing.T) {
	_, err := probe.ParseOutput(`regex:^available$`, "stopped\n", 0)
	if err == nil {
		t.Fatal("expected error when regex does not match")
	}
}

func TestParseOutput_Regex_MultipleGroups(t *testing.T) {
	// Returns first capture group only.
	v, err := probe.ParseOutput(`regex:(\w+)\s+(\d+)`, "count 99\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "count")
}

func TestParseOutput_Regex_InvalidPattern(t *testing.T) {
	_, err := probe.ParseOutput(`regex:[invalid`, "any\n", 0)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — lines
// ---------------------------------------------------------------------------

func TestParseOutput_Lines(t *testing.T) {
	v, err := probe.ParseOutput("lines:1", "10.0.1.1\n10.0.1.2\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 2)
}

func TestParseOutput_Lines_Empty(t *testing.T) {
	v, err := probe.ParseOutput("lines:1", "\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 0)
}

func TestParseOutput_Lines_SingleLine(t *testing.T) {
	v, err := probe.ParseOutput("lines:1", "10.0.1.1\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 1)
}

func TestParseOutput_Lines_BlankLines(t *testing.T) {
	v, err := probe.ParseOutput("lines:1", "\n\n10.0.1.1\n\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Only the one non-empty line counts.
	assertEqual(t, v, 1)
}

// ---------------------------------------------------------------------------
// ParseOutput — json
// ---------------------------------------------------------------------------

func TestParseOutput_JSON_Field(t *testing.T) {
	v, err := probe.ParseOutput("json:.status", `{"status": "ok"}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "ok")
}

func TestParseOutput_JSON_Nested(t *testing.T) {
	v, err := probe.ParseOutput("json:.status.replicas", `{"status": {"replicas": 3}}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 3)
}

func TestParseOutput_JSON_ArrayIndex(t *testing.T) {
	v, err := probe.ParseOutput("json:.items.1", `{"items": ["a", "b", "c"]}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, "b")
}

func TestParseOutput_JSON_Length(t *testing.T) {
	v, err := probe.ParseOutput("json:.subsets.0.addresses|length", `{"subsets": [{"addresses": ["10.0.0.1", "10.0.0.2"]}]}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 2)
}

func TestParseOutput_JSON_InvalidJSON(t *testing.T) {
	_, err := probe.ParseOutput("json:.field", "not-json", 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseOutput_JSON_MissingKey(t *testing.T) {
	_, err := probe.ParseOutput("json:.missing", `{"status": "ok"}`, 0)
	if err == nil {
		t.Fatal("expected error for missing JSON key")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — age_seconds
// ---------------------------------------------------------------------------

func TestParseOutput_AgeSeconds(t *testing.T) {
	// Use a timestamp 120 seconds in the past.
	ts := time.Now().Add(-120 * time.Second).UTC().Format(time.RFC3339)
	v, err := probe.ParseOutput("age_seconds", ts+"\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	age, ok := v.(int)
	if !ok {
		t.Fatalf("expected int, got %T", v)
	}
	// Allow 5 seconds of clock skew.
	if age < 115 || age > 125 {
		t.Fatalf("expected age ~120, got %d", age)
	}
}

func TestParseOutput_AgeSeconds_Empty(t *testing.T) {
	// Empty timestamp (field not set) should return 0.
	v, err := probe.ParseOutput("age_seconds", "\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 0)
}

func TestParseOutput_AgeSeconds_InvalidFormat(t *testing.T) {
	_, err := probe.ParseOutput("age_seconds", "not-a-timestamp\n", 0)
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

// ---------------------------------------------------------------------------
// ParseOutput — unknown mode
// ---------------------------------------------------------------------------

func TestParseOutput_UnknownMode(t *testing.T) {
	_, err := probe.ParseOutput("unknown", "anything", 0)
	if err == nil {
		t.Fatal("expected error for unknown parse mode")
	}
}

// ---------------------------------------------------------------------------
// Substitute
// ---------------------------------------------------------------------------

func TestSubstitute(t *testing.T) {
	result := probe.Substitute(
		"kubectl -n {namespace} get deploy {name}",
		"api",
		map[string]string{"namespace": "production"},
		nil,
	)
	expected := "kubectl -n production get deploy api"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestSubstitute_ProviderVar(t *testing.T) {
	result := probe.Substitute(
		"aws {region} describe {name}",
		"mydb",
		map[string]string{},
		map[string]string{"region": "us-east-1"},
	)
	expected := "aws us-east-1 describe mydb"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestSubstitute_ModelOverridesProvider(t *testing.T) {
	result := probe.Substitute(
		"cmd {region} {name}",
		"svc",
		map[string]string{"region": "eu-west-1"},
		map[string]string{"region": "us-east-1"},
	)
	// modelVars takes precedence.
	expected := "cmd eu-west-1 svc"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestSubstitute_UnknownPlaceholderLeftAsIs(t *testing.T) {
	result := probe.Substitute(
		"kubectl {unknown} get {name}",
		"svc",
		map[string]string{},
		nil,
	)
	expected := "kubectl {unknown} get svc"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// ValidateCommand
// ---------------------------------------------------------------------------

func TestValidateCommand_Clean(t *testing.T) {
	err := probe.ValidateCommand(
		"kubectl -n production get deploy api",
		"kubectl -n {namespace} get deploy {name}",
	)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateCommand_InjectionSemicolon(t *testing.T) {
	// Simulates a namespace variable containing "; rm -rf /"
	rendered := "kubectl -n prod; rm -rf / get deploy api"
	template := "kubectl -n {namespace} get deploy {name}"
	err := probe.ValidateCommand(rendered, template)
	if err == nil {
		t.Fatal("expected injection error for semicolon")
	}
}

func TestValidateCommand_InjectionPipe(t *testing.T) {
	rendered := "kubectl -n prod | evil get deploy api"
	template := "kubectl -n {namespace} get deploy {name}"
	err := probe.ValidateCommand(rendered, template)
	if err == nil {
		t.Fatal("expected injection error for pipe")
	}
}

func TestValidateCommand_InjectionCommandSub(t *testing.T) {
	rendered := "kubectl -n $(cat /etc/passwd) get deploy api"
	template := "kubectl -n {namespace} get deploy {name}"
	err := probe.ValidateCommand(rendered, template)
	if err == nil {
		t.Fatal("expected injection error for command substitution")
	}
}

func TestValidateCommand_TemplateWithPipeIsOK(t *testing.T) {
	// Template already contains a pipe — rendered having the same count is fine.
	template := "aws cloudwatch get-metric | jq .value"
	rendered := "aws cloudwatch get-metric | jq .value"
	err := probe.ValidateCommand(rendered, template)
	if err != nil {
		t.Fatalf("unexpected error when pipe count matches template: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fixture backend
// ---------------------------------------------------------------------------

const fixturePath = "testdata/fixture.yaml"

func TestFixture_IntFact(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ex.Run(context.Background(), probe.Command{
		Provider:  "provider_a",
		Component: "comp_int",
		Fact:      "count",
		Parse:     "int",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Parsed != 42 {
		t.Fatalf("expected 42, got %v", result.Parsed)
	}
}

func TestFixture_BoolFact(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ex.Run(context.Background(), probe.Command{
		Provider:  "provider_b",
		Component: "comp_flags",
		Fact:      "available",
		Parse:     "bool",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, result.Parsed, true)
}

func TestFixture_FloatFact(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ex.Run(context.Background(), probe.Command{
		Provider:  "provider_b",
		Component: "comp_flags",
		Fact:      "rate",
		Parse:     "float",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAlmostEqual(t, result.Parsed, 498.5)
}

func TestFixture_JSONPathParse(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ex.Run(context.Background(), probe.Command{
		Provider:  "provider_a",
		Component: "comp_json",
		Fact:      "payload",
		Parse:     "json:.subsets.0.addresses|length",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, result.Parsed, 2)
}

func TestFixture_LinesParse(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ex.Run(context.Background(), probe.Command{
		Provider:  "provider_a",
		Component: "comp_lines",
		Fact:      "endpoints",
		Parse:     "lines:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, result.Parsed, 3)
}

func TestFixture_NotFound(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ex.Run(context.Background(), probe.Command{
		Provider:  "provider_a",
		Component: "comp_int",
		Fact:      "nonexistent_fact",
		Parse:     "int",
	})
	if err == nil {
		t.Fatal("expected error for missing fixture entry")
	}
}

func TestFixture_ProviderNotFound(t *testing.T) {
	ex, err := fixture.Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ex.Run(context.Background(), probe.Command{
		Provider:  "unknown_provider",
		Component: "comp_int",
		Fact:      "count",
		Parse:     "int",
	})
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestFixture_LoadMissingFile(t *testing.T) {
	_, err := fixture.Load("/nonexistent/path/fixture.yaml")
	if err == nil {
		t.Fatal("expected error loading missing fixture file")
	}
}
