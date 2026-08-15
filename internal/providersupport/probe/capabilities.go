// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Capabilities expansion for image-installed providers.
//
// Providers declare semantic labels ("kubectl", "aws", "docker", …) in
// their manifest.yaml. At probe dispatch time mgtt expands each label
// into the docker-run flags that actually grant the intent: bind mounts
// for credential dirs, -e flags for env vars, socket mounts for daemon
// access. Network mode (bridge/host) is a separate field on
// Provider.Runtime.NetworkMode, not a capability — see NewImageRunner
// in runner.go for how the two are composed.
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

// capSpec declaratively describes one capability: an optional $HOME-based
// read-only bind mount + env keys + env prefix matches. The `extra`
// closure is the escape hatch for capabilities whose expansion doesn't
// fit the table (terraform's cwd mount, docker's socket mount).
type capSpec struct {
	homeSubpath string   // path under $HOME to mount (e.g. ".kube"); empty = no mount
	mountPath   string   // path inside container (e.g. "/root/.kube")
	envKeys     []string // env vars to pass-through when set
	envPrefixes []string // env-key prefixes to pass-through (e.g. "TF_VAR_")
	extra       func() Capability
}

func (s capSpec) expand() Capability {
	out := Capability{}
	if s.homeSubpath != "" {
		if home := os.Getenv("HOME"); home != "" {
			out = append(out, "-v", home+"/"+s.homeSubpath+":"+s.mountPath+":ro")
		}
	}
	for _, k := range s.envKeys {
		out = append(out, passEnv(k)...)
	}
	if len(s.envPrefixes) > 0 {
		for _, kv := range os.Environ() {
			for _, p := range s.envPrefixes {
				if strings.HasPrefix(kv, p) {
					out = append(out, "-e", kv)
					break
				}
			}
		}
	}
	if s.extra != nil {
		out = append(out, s.extra()...)
	}
	return out
}

// builtins is the frozen starter vocabulary. Adding a capability is one
// row in the table; prefer read-only bind mounts; env passthrough only
// emits -e KEY=VALUE when KEY is set in the mgtt process. Network mode
// (bridge/host) is not a capability — see Provider.Runtime.NetworkMode
// and NewImageRunner.
var builtinSpecs = map[string]capSpec{
	"kubectl": {homeSubpath: ".kube", mountPath: "/root/.kube", envKeys: []string{"KUBECONFIG"}},
	"aws": {
		homeSubpath: ".aws", mountPath: "/root/.aws",
		envKeys: []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION", "AWS_DEFAULT_REGION"},
	},
	"docker": {
		extra: func() Capability { return Capability{"-v", "/var/run/docker.sock:/var/run/docker.sock"} },
	},
	"terraform": {
		envKeys:     []string{"TF_CLI_CONFIG_FILE"},
		envPrefixes: []string{"TF_VAR_"},
		extra: func() Capability {
			if pwd, err := os.Getwd(); err == nil && pwd != "" {
				return Capability{"-v", pwd + ":/workspace", "-w", "/workspace"}
			}
			return nil
		},
	},
	"gcloud": {
		homeSubpath: ".config/gcloud", mountPath: "/root/.config/gcloud",
		envKeys:     []string{"GOOGLE_APPLICATION_CREDENTIALS"},
		envPrefixes: []string{"CLOUDSDK_"},
	},
	"azure": {
		homeSubpath: ".azure", mountPath: "/root/.azure",
		envKeys: []string{"ARM_CLIENT_ID", "ARM_CLIENT_SECRET", "ARM_TENANT_ID", "ARM_SUBSCRIPTION_ID"},
	},
}

// builtins is the map form the rest of the package reads. Preserved as
// a map of generators so the override/cache machinery keeps working.
var builtins = func() map[string]func() Capability {
	out := make(map[string]func() Capability, len(builtinSpecs))
	for name, spec := range builtinSpecs {
		spec := spec
		out[name] = spec.expand
	}
	return out
}()

// passEnv returns ["-e", "KEY=VALUE"] when KEY is set to a non-empty
// value, else nil. mgtt never emits a bare -e flag (which would make
// Docker consume the next positional arg), and treats KEY="" the same
// as unset because most callers consider an empty var meaningless.
func passEnv(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
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

// Apply expands a list of needs into docker-run argv, honoring
// MGTT_IMAGE_CAPS_DENY. The return value is ready to prepend to
// `<imageRef> <probe-args>`.
//
// Order is stable: caps expand in the order manifest.yaml declares them,
// so operators reading the docker-run line can scan needs and argv side
// by side.
//
// Unknown capabilities are skipped but emit a stderr warning so the
// operator learns about it — install-time validation is the loud path,
// but this is defense in depth in case a capabilities.yaml override was
// removed between install and probe.
func Apply(needs []string) []string {
	deny := parseDeny(os.Getenv("MGTT_IMAGE_CAPS_DENY"))
	var out []string
	for _, n := range needs {
		if deny[n] {
			continue
		}
		cap, ok := resolve(n)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"mgtt: warning: unknown image capability %q — skipping (known: %s)\n",
				n, strings.Join(KnownNames(), ", "))
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

// overridesMu guards the (once, map) pair. sync.Once alone isn't
// enough: `loadedOverridesOnce = sync.Once{}` on reset is a
// non-atomic write to a struct a concurrent loadOverrides might be
// reading. A mutex around both read and reset closes that window.
var (
	overridesMu         sync.Mutex
	loadedOverridesOnce sync.Once
	loadedOverrides     map[string]Capability
)

// resetOverridesCacheForTest clears the once-loaded cache. Tests that
// mutate MGTT_HOME or capabilities.yaml files between runs call this via
// the exported ResetOverridesCache to force a reload.
func resetOverridesCacheForTest() {
	overridesMu.Lock()
	defer overridesMu.Unlock()
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
	overridesMu.Lock()
	defer overridesMu.Unlock()
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
