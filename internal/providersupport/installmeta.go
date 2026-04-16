package providersupport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// InstallMethod records how a provider was installed.
type InstallMethod string

const (
	InstallMethodGit   InstallMethod = "git"
	InstallMethodImage InstallMethod = "image"
)

// installMetaFilename is written into ~/.mgtt/providers/<name>/ at install
// time and consulted by the CLI (list, uninstall, plan) to decide how to
// invoke the provider and how to clean it up.
const installMetaFilename = ".mgtt-install.json"

// InstallMeta is what we persist per provider after install.
type InstallMeta struct {
	// Method is "git" or "image".
	Method InstallMethod `json:"method"`
	// Namespace is the registry namespace the provider was pulled from
	// (e.g. "mgt-tool"). Empty for legacy git-installed providers.
	Namespace string `json:"namespace,omitempty"`
	// Source is the git URL (for git installs) or image ref including digest
	// (for image installs). For image installs, callers MUST include @sha256:
	// — the install command rejects bare tags.
	Source string `json:"source"`
	// InstalledAt is the wall-clock time of install (UTC, second precision).
	InstalledAt time.Time `json:"installed_at"`
	// Version is the Meta.Version from the provider.yaml at install time.
	// Used only for `mgtt provider list` output today; informational.
	Version string `json:"version,omitempty"`
}

// WriteInstallMeta persists meta into <providerDir>/.mgtt-install.json.
func WriteInstallMeta(providerDir string, m InstallMeta) error {
	path := filepath.Join(providerDir, installMetaFilename)
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal install meta: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write install meta: %w", err)
	}
	return nil
}

// ReadInstallMeta returns the meta written by a previous install.
// If the file is absent, returns InstallMeta{Method: InstallMethodGit} for
// backward compatibility — providers installed before this feature shipped
// have no metadata file but ARE git-installed.
func ReadInstallMeta(providerDir string) (InstallMeta, error) {
	path := filepath.Join(providerDir, installMetaFilename)
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return InstallMeta{Method: InstallMethodGit}, nil
		}
		return InstallMeta{}, fmt.Errorf("read install meta: %w", err)
	}
	var m InstallMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return InstallMeta{}, fmt.Errorf("parse install meta: %w", err)
	}
	return m, nil
}
