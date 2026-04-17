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

	// ReadOnly is the provider's declared write posture.
	//
	// true  — the provider only reads; no side effects. This is the
	//         default when `read_only:` is absent from manifest.yaml.
	// false — the provider has side effects; WritesNote must describe them.
	//         `mgtt provider install` prints the note so the operator
	//         consents knowingly. Validation rejects `read_only: false`
	//         without an accompanying WritesNote.
	ReadOnly bool

	// WritesNote explains the side effect when ReadOnly is false. Ignored
	// when ReadOnly is true. Free-form markdown — operators read it.
	WritesNote string

	// Needs lists named capabilities the provider requires at probe time —
	// each label names a host-side package, credential chain, or socket
	// (kubectl, aws, docker, terraform, gcloud, azure). Git installs
	// satisfy needs by inheriting the operator's shell environment;
	// image installs satisfy them via docker-run bind mounts and env
	// forwards built by internal/providersupport/probe/capabilities.go.
	// Populated from the top-level `needs:` block in manifest.yaml.
	Needs []string

	// Network selects the docker-run network mode for image-installed
	// providers. Valid values: "bridge" (default), "host", "none".
	// Separate from Needs because network mode is a runtime isolation
	// setting, not a host-side resource grant — mixing the two under
	// one key conflated categories. Empty string defaults to "bridge".
	Network string
}

type ProviderMeta struct {
	Name        string
	Version     string
	Description string
	Tags        []string          // loose subject/topic labels — what the provider is about
	Command     string            // path to provider binary; may contain $MGTT_PROVIDER_DIR
	Requires    map[string]string // dependency constraints, e.g. {"mgtt": ">=0.1.0"}
}

type ProviderHooks struct {
	Install   string
	Uninstall string
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

func ptr(f float64) *float64 { return &f }
