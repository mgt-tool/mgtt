package providersupport

import (
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
)

type Provider struct {
	Meta      ProviderMeta
	Hooks     ProviderHooks
	Types     map[string]*Type
	Variables map[string]Variable
	Auth      AuthSpec
}

type ProviderMeta struct {
	Name        string
	Version     string
	Description string
	Command     string // path to provider binary; may contain $MGTT_PROVIDER_DIR
}

type ProviderHooks struct {
	Install string
}

type DataType struct {
	Name    string
	Base    string   // stdlib primitive: "int", "float", "bool", "string"
	Units   []string // valid suffixes; nil for unitless
	Range   *Range
	Default interface{}
}

type Range struct {
	Min *float64
	Max *float64
}

type Type struct {
	Name               string
	Description        string
	Facts              map[string]*FactSpec
	HealthyRaw         []string
	Healthy            []expr.Node
	States             []StateDef // declaration order matters
	DefaultActiveState string
	FailureModes       map[string][]string // state → can_cause
}

type FactSpec struct {
	TypeName string
	TTL      time.Duration
	Probe    ProbeDef
}

type ProbeDef struct {
	Cmd     string
	Parse   string
	Cost    string
	Access  string
	Timeout time.Duration
}

type StateDef struct {
	Name        string
	WhenRaw     string
	When        expr.Node // compiled from WhenRaw; nil if WhenRaw is empty
	Description string
}

type Variable struct {
	Description string
	Required    bool
	Default     string
}

type AuthSpec struct {
	Strategy string
	Access   AuthAccess
}

type AuthAccess struct {
	Probes string
	Writes string
}

func ptr(f float64) *float64 { return &f }
