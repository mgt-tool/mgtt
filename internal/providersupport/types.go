package providersupport

import (
	"time"

	"github.com/sajonaro/mgtt/internal/expr"
)

// Provider is the in-memory representation of a loaded provider definition.
type Provider struct {
	Meta      ProviderMeta
	Hooks     ProviderHooks
	DataTypes map[string]DataType
	Types     map[string]*Type
	Variables map[string]Variable
	Auth      AuthSpec
}

// ProviderMeta holds top-level metadata for a provider.
type ProviderMeta struct {
	Name        string
	Version     string
	Description string
	Requires    map[string]string
	Command     string // path to provider binary; may contain $MGTT_PROVIDER_DIR
}

// ProviderHooks holds lifecycle hook paths declared by a provider.
type ProviderHooks struct {
	Install string
	Update  string
}

// DataType describes a named data type with an optional unit system and range.
type DataType struct {
	Name    string
	Base    string     // stdlib primitive: "int", "float", "bool", "string"
	Units   []string   // valid suffixes; nil for unitless
	Range   *Range
	Default interface{}
}

// Range is an optional inclusive numeric boundary for a DataType.
type Range struct {
	Min *float64
	Max *float64
}

// Type describes a component type provided by a provider.
type Type struct {
	Name               string
	Description        string
	Facts              map[string]*FactSpec
	HealthyRaw         []string   // raw expression strings
	Healthy            []expr.Node // compiled from HealthyRaw
	States             []StateDef  // ordered — declaration order matters!
	DefaultActiveState string
	FailureModes       map[string][]string // state → can_cause
}

// FactSpec describes a single observable fact about a component.
type FactSpec struct {
	TypeName string // "mgtt.int", "mgtt.bool", etc.
	TTL      time.Duration
	Probe    ProbeDef
	Default  interface{}
}

// ProbeDef describes how to collect a fact value.
type ProbeDef struct {
	Cmd     string
	Parse   string // "int", "float", "bool", "string", "exit_code", "json:path", "lines:N", "regex:pat"
	Cost    string // "low" | "medium" | "high"
	Access  string
	Timeout time.Duration
}

// StateDef represents a named state with a when-expression.
type StateDef struct {
	Name        string
	WhenRaw     string    // raw expression, compiled later
	When        expr.Node // compiled from WhenRaw; nil if WhenRaw is empty
	Description string
}

// Variable is a provider-level variable that can be overridden by the model.
type Variable struct {
	Description string
	Required    bool
	Default     string
}

// AuthSpec describes how the provider authenticates.
type AuthSpec struct {
	Strategy  string
	ReadsFrom []string
	Access    AuthAccess
}

// AuthAccess describes what level of access probes and writes require.
type AuthAccess struct {
	Probes string
	Writes string
}

// ptr returns a pointer to the given float64 value.
func ptr(f float64) *float64 { return &f }
