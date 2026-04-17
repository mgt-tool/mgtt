// Capabilities expansion for image-installed providers.
//
// Providers declare semantic labels ("kubectl", "docker", "network", …) in
// their provider.yaml. At probe dispatch time mgtt expands each label into
// the docker-run flags that actually grant the intent: bind mounts for
// credential dirs, -e flags for env vars, --network host when the probe
// must reach an in-cluster URL, etc.
//
// The vocabulary is closed and lives here. Operators with non-default
// paths or a need for a capability mgtt doesn't ship with can override or
// extend via $MGTT_HOME/capabilities.yaml and MGTT_IMAGE_CAP_<NAME> env
// vars; see loadOverrides.

package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Capability is the expansion of one named cap into docker-run argv.
// Entries are plain strings already split — no shell re-parsing.
type Capability []string

// builtins is the frozen starter vocabulary. Adding a capability is a
// one-line append here. Prefer read-only bind mounts; env passthrough
// only emits -e KEY=VALUE when KEY is set in the mgtt process.
var builtins = map[string]func() Capability{
	"network": func() Capability {
		return Capability{"--network", "host"}
	},
	"kubectl": func() Capability {
		home := os.Getenv("HOME")
		out := Capability{}
		if home != "" {
			out = append(out, "-v", home+"/.kube:/root/.kube:ro")
		}
		return append(out, passEnv("KUBECONFIG")...)
	},
	"aws": func() Capability {
		home := os.Getenv("HOME")
		out := Capability{}
		if home != "" {
			out = append(out, "-v", home+"/.aws:/root/.aws:ro")
		}
		for _, k := range []string{
			"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
			"AWS_SESSION_TOKEN", "AWS_REGION", "AWS_DEFAULT_REGION",
		} {
			out = append(out, passEnv(k)...)
		}
		return out
	},
	"docker": func() Capability {
		return Capability{"-v", "/var/run/docker.sock:/var/run/docker.sock"}
	},
	"terraform": func() Capability {
		out := Capability{}
		if pwd, err := os.Getwd(); err == nil && pwd != "" {
			out = append(out, "-v", pwd+":/workspace", "-w", "/workspace")
		}
		out = append(out, passEnv("TF_CLI_CONFIG_FILE")...)
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TF_VAR_") {
				out = append(out, "-e", kv)
			}
		}
		return out
	},
	"gcloud": func() Capability {
		home := os.Getenv("HOME")
		out := Capability{}
		if home != "" {
			out = append(out, "-v", home+"/.config/gcloud:/root/.config/gcloud:ro")
		}
		out = append(out, passEnv("GOOGLE_APPLICATION_CREDENTIALS")...)
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "CLOUDSDK_") {
				out = append(out, "-e", kv)
			}
		}
		return out
	},
	"azure": func() Capability {
		home := os.Getenv("HOME")
		out := Capability{}
		if home != "" {
			out = append(out, "-v", home+"/.azure:/root/.azure:ro")
		}
		for _, k := range []string{
			"ARM_CLIENT_ID", "ARM_CLIENT_SECRET", "ARM_TENANT_ID", "ARM_SUBSCRIPTION_ID",
		} {
			out = append(out, passEnv(k)...)
		}
		return out
	},
}

// passEnv returns ["-e", "KEY=VALUE"] when KEY is set, else nil. mgtt never
// emits a bare -e flag to avoid Docker consuming the next arg.
func passEnv(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	return []string{"-e", key + "=" + v}
}

// Known reports whether the named capability resolves against the merged
// map (builtins ∪ operator file ∪ env overrides). Called by validation.
func Known(name string) bool {
	_, ok := resolve(name)
	return ok
}

// KnownNames returns the sorted union of built-ins and operator-declared
// names. Used by validate error messages and the install-time print.
func KnownNames() []string {
	set := map[string]struct{}{}
	for k := range builtins {
		set[k] = struct{}{}
	}
	for k := range loadOverrides() {
		set[k] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Apply expands a list of needs into docker-run argv, skipping unknown
// caps and honoring MGTT_IMAGE_CAPS_DENY. The return value is ready to
// prepend to `<imageRef> <probe-args>`.
//
// Order is stable: caps expand in the order provider.yaml declares them,
// so operators reading the docker-run line can scan needs and argv side
// by side.
func Apply(needs []string) []string {
	deny := parseDeny(os.Getenv("MGTT_IMAGE_CAPS_DENY"))
	var out []string
	for _, n := range needs {
		if deny[n] {
			continue
		}
		cap, ok := resolve(n)
		if !ok {
			continue
		}
		out = append(out, cap...)
	}
	return out
}

// resolve is the precedence chain: env override > operator file > builtins.
func resolve(name string) (Capability, bool) {
	if v := os.Getenv("MGTT_IMAGE_CAP_" + strings.ToUpper(name)); v != "" {
		return Capability(splitShell(v)), true
	}
	if over, ok := loadOverrides()[name]; ok {
		return over, true
	}
	if fn, ok := builtins[name]; ok {
		return fn(), true
	}
	return nil, false
}

func parseDeny(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

// splitShell does a minimal shell-style split on whitespace, preserving
// quoted substrings. Good enough for MGTT_IMAGE_CAP_* one-liners.
func splitShell(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

var (
	loadedOverridesOnce sync.Once
	loadedOverrides     map[string]Capability
)

// resetOverridesCacheForTest clears the once-loaded cache. Tests that
// mutate MGTT_HOME or capabilities.yaml files between runs call this via
// the exported ResetOverridesCache to force a reload.
func resetOverridesCacheForTest() {
	loadedOverridesOnce = sync.Once{}
	loadedOverrides = nil
}

// ResetOverridesCache clears the cached operator-overrides map so the
// next call to Apply / Known / KnownNames re-reads $MGTT_HOME/capabilities.yaml.
// Exported only for tests and short-lived CLI invocations that may
// legitimately re-read config.
func ResetOverridesCache() { resetOverridesCacheForTest() }

// loadOverrides reads $MGTT_HOME/capabilities.yaml and any drop-in shards
// under $MGTT_HOME/capabilities.d/*.yaml once per process. Returns an
// empty map on any read/parse failure — errors are intentionally silent
// at probe time (install/validate is the loud path).
func loadOverrides() map[string]Capability {
	loadedOverridesOnce.Do(func() {
		loadedOverrides = map[string]Capability{}
		root := mgttHome()
		if root == "" {
			return
		}
		paths := []string{filepath.Join(root, "capabilities.yaml")}
		if entries, err := os.ReadDir(filepath.Join(root, "capabilities.d")); err == nil {
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
					continue
				}
				paths = append(paths, filepath.Join(root, "capabilities.d", e.Name()))
			}
		}
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			m, err := parseCapabilitiesYAML(data)
			if err != nil {
				continue
			}
			for k, v := range m {
				loadedOverrides[k] = v
			}
		}
	})
	return loadedOverrides
}

// parseCapabilitiesYAML is extracted for testability.
func parseCapabilitiesYAML(data []byte) (map[string]Capability, error) {
	var raw struct {
		Capabilities map[string][]string `yaml:"capabilities"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse capabilities.yaml: %w", err)
	}
	out := map[string]Capability{}
	for k, v := range raw.Capabilities {
		out[k] = Capability(v)
	}
	return out, nil
}

// mgttHome returns $MGTT_HOME if set, else $HOME/.mgtt, else "".
func mgttHome() string {
	if h := os.Getenv("MGTT_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".mgtt")
	}
	return ""
}
