package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_BuiltinKubectl(t *testing.T) {
	t.Setenv("HOME", "/home/alice")
	t.Setenv("MGTT_HOME", t.TempDir()) // no overrides
	ResetOverridesCache()

	got := Apply([]string{"kubectl"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-v /home/alice/.kube:/root/.kube:ro") {
		t.Errorf("kubectl cap must mount ~/.kube readonly; got %v", got)
	}
}

func TestApply_BuiltinNetwork(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	got := Apply([]string{"network"})
	if len(got) != 2 || got[0] != "--network" || got[1] != "host" {
		t.Errorf("network cap must be --network host; got %v", got)
	}
}

func TestApply_BuiltinDockerSocket(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	got := Apply([]string{"docker"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-v /var/run/docker.sock:/var/run/docker.sock") {
		t.Errorf("docker cap must mount the socket; got %v", got)
	}
}

func TestApply_UnknownCapSkipped(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	got := Apply([]string{"kubectl", "vault-undefined", "network"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--network host") {
		t.Errorf("known caps must still expand when an unknown is skipped; got %v", got)
	}
	if strings.Contains(joined, "vault-undefined") {
		t.Errorf("unknown cap must not leak into argv; got %v", got)
	}
}

func TestApply_CapsDenyFilters(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	t.Setenv("MGTT_IMAGE_CAPS_DENY", "docker,network")
	got := Apply([]string{"kubectl", "docker", "network"})
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "/var/run/docker.sock") {
		t.Errorf("docker cap must be filtered by DENY; got %v", got)
	}
	if strings.Contains(joined, "--network") {
		t.Errorf("network cap must be filtered by DENY; got %v", got)
	}
	if !strings.Contains(joined, ".kube") {
		t.Errorf("kubectl cap must survive; got %v", got)
	}
}

func TestApply_EnvPassthroughOnlyWhenSet(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	t.Setenv("AWS_PROFILE", "dev")
	os.Unsetenv("AWS_SESSION_TOKEN")
	got := Apply([]string{"aws"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-e AWS_PROFILE=dev") {
		t.Errorf("set env must be passed through as KEY=VALUE; got %v", got)
	}
	if strings.Contains(joined, "AWS_SESSION_TOKEN") {
		t.Errorf("unset env must not produce -e flag; got %v", got)
	}
}

func TestKnown(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	ResetOverridesCache()
	if !Known("kubectl") {
		t.Error("Known(kubectl) must be true for built-in")
	}
	if Known("vault-undefined") {
		t.Error("Known must return false for undefined cap")
	}
}

func TestApply_OperatorFileOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	y := `
capabilities:
  kubectl:
    - "-v"
    - "/etc/kubernetes/admin.conf:/root/.kube/config:ro"
    - "-e"
    - "KUBECONFIG=/root/.kube/config"
  vault:
    - "-v"
    - "/opt/vault:/root/.vault:ro"
`
	if err := os.WriteFile(filepath.Join(home, "capabilities.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	ResetOverridesCache()

	got := Apply([]string{"kubectl", "vault"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "/etc/kubernetes/admin.conf") {
		t.Errorf("operator override must win over built-in; got %v", got)
	}
	if !strings.Contains(joined, "/opt/vault:/root/.vault:ro") {
		t.Errorf("operator-defined custom cap must expand; got %v", got)
	}
	if !Known("vault") {
		t.Error("Known must include operator-defined caps")
	}
}

func TestApply_EnvOverrideWinsOverFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "capabilities.yaml"), []byte(`
capabilities:
  kubectl: ["-v", "/file/path:/root/.kube:ro"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ResetOverridesCache()
	t.Setenv("MGTT_IMAGE_CAP_KUBECTL", "-v /env/path:/root/.kube:ro")

	got := Apply([]string{"kubectl"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "/env/path:/root/.kube:ro") {
		t.Errorf("env override must win over file; got %v", got)
	}
	if strings.Contains(joined, "/file/path") {
		t.Errorf("file path must not leak when env is set; got %v", got)
	}
}

func TestApply_DropInShardsLoaded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	if err := os.Mkdir(filepath.Join(home, "capabilities.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "capabilities.d", "tibco.yaml"), []byte(`
capabilities:
  tibco: ["-v", "/etc/tibco:/root/.tibco:ro"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ResetOverridesCache()

	got := Apply([]string{"tibco"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "/etc/tibco:/root/.tibco:ro") {
		t.Errorf("drop-in shard must register custom cap; got %v", got)
	}
}

func TestSplitShell(t *testing.T) {
	cases := map[string][]string{
		``:                                nil,
		`-v a:b -e X`:                     {"-v", "a:b", "-e", "X"},
		`-v "a b:c" -e Y`:                 {"-v", "a b:c", "-e", "Y"},
		`-e 'K=V with spaces' -e K2=V2`:   {"-e", "K=V with spaces", "-e", "K2=V2"},
	}
	for in, want := range cases {
		got := splitShell(in)
		if len(got) != len(want) {
			t.Errorf("splitShell(%q) = %v; want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitShell(%q)[%d] = %q; want %q", in, i, got[i], want[i])
			}
		}
	}
}
