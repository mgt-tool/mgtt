package providersupport

import (
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
)

// Provider is the in-memory representation of a manifest.yaml (v1.0
// schema). Three sub-structs carry the top-level blocks; ReadOnly /
// WritesNote remain top-level because posture is a whole-provider fact.
type Provider struct {
	Meta    ProviderMeta
	Runtime ProviderRuntime
	Install ProviderInstall

	Types     map[string]*Type
	Variables map[string]Variable

	// ReadOnly is the provider's declared write posture (default true).
	ReadOnly bool
	// WritesNote describes side effects when ReadOnly is false.
	WritesNote string
}

// ResolveEntrypoint returns the binary invocation string mgtt should use
// at probe time, honoring Runtime.Entrypoint when the author declared it
// and falling back to the convention otherwise.
//
// providerDir is the directory the provider lives in on disk (for source
// install) or empty (for image install — the caller is expected to use
// the image's baked-in ENTRYPOINT when this returns "").
func (p *Provider) ResolveEntrypoint(method InstallMethod, providerDir string) string {
	if p.Runtime.Entrypoint != "" {
		return p.Runtime.Entrypoint
	}
	if method == InstallMethodImage {
		return ""
	}
	return providerDir + "/bin/mgtt-provider-" + p.Meta.Name
}

// ProviderMeta is the identity block — who this provider is.
type ProviderMeta struct {
	Name        string
	Version     string
	Description string
	Tags        []string
	Requires    map[string]string // e.g. {"mgtt": ">=0.2.0"}
}

// ProviderRuntime is how the provider talks to its backend and how mgtt
// invokes it at probe time.
type ProviderRuntime struct {
	// Needs maps capability vocabulary keys to optional semver range
	// constraints on the backing tool. Empty string value means "any
	// version is fine".
	Needs map[string]string

	// Backends maps backend-service names to optional semver range
	// constraints. Separate axis from Needs (author declares
	// compatibility with the upstream service, not operator tooling).
	Backends map[string]string

	// NetworkMode is the docker-run --network for image installs.
	// Values: "bridge" (default) | "host". Empty string means "bridge".
	NetworkMode string

	// Entrypoint overrides the convention-derived invocation path.
	// Empty string means "use the default": for source installs
	// $MGTT_PROVIDER_DIR/bin/mgtt-provider-<Meta.Name>; for image
	// installs the image's baked-in ENTRYPOINT.
	Entrypoint string
}

// ProviderInstall declares which install methods the provider supports.
// At least one of Source or Image must be populated after parse.
type ProviderInstall struct {
	Source *InstallSource // nil => source install not offered
	Image  *InstallImage  // nil => image install not offered
}

// InstallSource describes source-install mechanics. Both fields required
// when Source is non-nil.
type InstallSource struct {
	Build string // path to build script (relative to manifest dir)
	Clean string // path to clean script (relative to manifest dir)
}

// InstallImage describes image-install mechanics. Repository is optional;
// empty string means the parser should resolve from registry context.
type InstallImage struct {
	Repository string
}

type DataType struct {
	Name    string
	Base    string
	Units   []string
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
	States             []StateDef
	DefaultActiveState string
	FailureModes       map[string][]string
	// SourcePath is the on-disk path the type was loaded from, when
	// known. Set by LoadFromDir for types in the types/ subdirectory;
	// for inline types or tests it stays empty. Consumers that need a
	// stable hash should fall back to content-hashing when this is "".
	SourcePath string
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
	When        expr.Node
	Description string
	TriggeredBy []string
}

type Variable struct {
	Description string
	Required    bool
	Default     string
}

func ptr(f float64) *float64 { return &f }
