//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestImageInstall_FullRoundTrip exercises the complete image-install lifecycle:
//  1. Start a local Docker registry.
//  2. Build a fixture provider image from testdata/fixture-provider/.
//  3. Push to the local registry and obtain a @sha256: manifest digest.
//  4. Install it via `mgtt provider install --image <ref>`.
//  5. Verify the install metadata on disk.
//  6. Verify `mgtt provider ls` shows the provider with method=image.
//  6b. Run `mgtt plan` against a fixture model to exercise buildExecutor's image branch.
//  7. Invoke the image binary directly (docker run) to confirm the probe protocol works.
//  8. Uninstall and verify cleanup + "docker rmi" hint.
func TestImageInstall_FullRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	ctx := t.Context()

	// Resolve the repo root (two levels up from test/integration/).
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	// 1. Start a local Docker registry on a fixed port.
	// Port 15432 is chosen to be well outside the ephemeral range (32768-60999
	// on Linux). If two test runs overlap on the same machine (e.g. parallel CI
	// jobs), one will fail to bind; the fix is to pick an ephemeral port by
	// listening on :0 and passing the assigned port to `docker run -p`. That
	// is tracked as a follow-up; for now the fixed port is acceptable for
	// single-job CI.
	registryPort := "15432"
	registryHost := fmt.Sprintf("localhost:%s", registryPort)
	registryName := "mgtt-it-registry"

	// Remove any pre-existing registry container from a prior run.
	_ = exec.Command("docker", "rm", "-f", registryName).Run()

	regStart := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", registryName,
		"-p", registryPort+":5000",
		"registry:2",
	)
	if out, err := regStart.CombinedOutput(); err != nil {
		t.Fatalf("start local registry: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", registryName).Run()
	})

	// Wait for registry to be ready (up to 30s).
	registryReady := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:" + registryPort + "/v2/")
		if err == nil {
			resp.Body.Close()
			registryReady = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !registryReady {
		t.Fatal("registry did not become ready within 30s")
	}

	// 2. Build the fixture image.
	// The Dockerfile copies from the repo root so it can resolve the local
	// replace directive in the fixture's go.mod. Build context = repo root.
	dockerfilePath := filepath.Join(repoRoot, "test/integration/testdata/fixture-provider/Dockerfile")
	localTag := "mgtt-it-fixture-provider:latest"
	registryTag := fmt.Sprintf("%s/mgtt-it-fixture-provider:latest", registryHost)

	if out, err := exec.CommandContext(ctx, "docker", "build",
		"-t", localTag,
		"-f", dockerfilePath,
		repoRoot,
	).CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", localTag).Run() })

	// 3. Tag and push to the local registry to get a real manifest digest.
	if out, err := exec.CommandContext(ctx, "docker", "tag", localTag, registryTag).CombinedOutput(); err != nil {
		t.Fatalf("tag image: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", registryTag).Run() })

	if out, err := exec.CommandContext(ctx, "docker", "push", registryTag).CombinedOutput(); err != nil {
		t.Fatalf("push image to local registry: %v\n%s", err, out)
	}

	// Retrieve the manifest digest from the registry.
	digestOut, err := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{index .RepoDigests 0}}",
		registryTag,
	).Output()
	if err != nil {
		t.Fatalf("inspect registry tag for digest: %v", err)
	}
	digestRef := strings.TrimSpace(string(digestOut))
	if digestRef == "" || !strings.Contains(digestRef, "@sha256:") {
		// Fallback: parse digest from push output or use local image ID.
		// After docker push, docker inspect on the remote tag should show RepoDigests.
		// Force a re-pull to populate RepoDigests if not set yet.
		if out, err := exec.CommandContext(ctx, "docker", "pull", registryTag).CombinedOutput(); err != nil {
			t.Fatalf("pull from local registry (to populate digest): %v\n%s", err, out)
		}
		digestOut2, err := exec.CommandContext(ctx, "docker", "inspect",
			"--format", "{{index .RepoDigests 0}}",
			registryTag,
		).Output()
		if err != nil {
			t.Fatalf("inspect for digest after pull: %v", err)
		}
		digestRef = strings.TrimSpace(string(digestOut2))
	}
	if !strings.Contains(digestRef, "@sha256:") {
		t.Fatalf("could not obtain @sha256: digest ref; got %q", digestRef)
	}
	t.Logf("digestRef = %s", digestRef)

	// 4. Build the mgtt binary from the repo root.
	mgttBin := filepath.Join(t.TempDir(), "mgtt")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", mgttBin, "./cmd/mgtt")
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build mgtt: %v\n%s", err, out)
	}

	// 5. Install from the image.
	mgttHome := t.TempDir()
	cmd := exec.CommandContext(ctx, mgttBin, "provider", "install", "--image", digestRef, "fixture")
	cmd.Env = append(os.Environ(), "MGTT_HOME="+mgttHome)
	cmd.Dir = repoRoot
	installOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, installOut)
	}
	t.Logf("install output:\n%s", installOut)

	// 5a. Verify the install directory and metadata on disk.
	providerDir := filepath.Join(mgttHome, "providers", "fixture")
	metaBytes, err := os.ReadFile(filepath.Join(providerDir, ".mgtt-install.json"))
	if err != nil {
		t.Fatalf("read install meta: %v", err)
	}
	var meta struct {
		Method string `json:"method"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse install meta: %v", err)
	}
	if meta.Method != "image" {
		t.Errorf("method: want 'image', got %q", meta.Method)
	}
	if meta.Source != digestRef {
		t.Errorf("source: want %q, got %q", digestRef, meta.Source)
	}

	// 5b. Verify provider.yaml was written to the install dir.
	if _, err := os.Stat(filepath.Join(providerDir, "provider.yaml")); err != nil {
		t.Errorf("provider.yaml missing from install dir: %v", err)
	}

	// 6. `mgtt provider ls` shows the provider with method=image.
	cmd = exec.CommandContext(ctx, mgttBin, "provider", "ls")
	cmd.Env = append(os.Environ(), "MGTT_HOME="+mgttHome)
	cmd.Dir = repoRoot
	listOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider ls: %v\n%s", err, listOut)
	}
	t.Logf("provider ls output:\n%s", listOut)
	if !strings.Contains(string(listOut), "image") {
		t.Errorf("provider ls output does not contain 'image':\n%s", listOut)
	}
	if !strings.Contains(string(listOut), "fixture") {
		t.Errorf("provider ls output does not contain 'fixture':\n%s", listOut)
	}

	// 6b. Run `mgtt plan` against a trivial fixture model to exercise the full
	// buildExecutor → NewImageRunner → docker-run path (not just the binary
	// directly). The model names the installed 'fixture' provider and declares a
	// single 'my-widget' component of type 'widget'. Since the test runs under
	// exec.Command, isInteractive() returns false and probes are auto-accepted.
	fixtureModelPath := filepath.Join(t.TempDir(), "fixture.model.yaml")
	fixtureModel := `meta:
  name: fixture-test
  version: "1.0"
  providers: [fixture]

components:
  my-widget:
    type: widget
    providers: [fixture]
    vars: {}
`
	if err := os.WriteFile(fixtureModelPath, []byte(fixtureModel), 0o644); err != nil {
		t.Fatalf("write fixture model: %v", err)
	}
	cmd = exec.CommandContext(ctx, mgttBin, "plan", "--model", fixtureModelPath, "--component", "my-widget")
	cmd.Env = append(os.Environ(), "MGTT_HOME="+mgttHome)
	cmd.Dir = repoRoot
	planOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mgtt plan: %v\n%s", err, planOut)
	}
	t.Logf("mgtt plan output:\n%s", planOut)
	// The plan output should show the probe result (count=42) and the component
	// resolving to healthy (count > 0 is satisfied).
	if !strings.Contains(string(planOut), "my-widget") {
		t.Errorf("mgtt plan output missing 'my-widget':\n%s", planOut)
	}
	if !strings.Contains(string(planOut), "42") {
		t.Errorf("mgtt plan output missing probe value '42':\n%s", planOut)
	}
	if !strings.Contains(string(planOut), "healthy") {
		t.Errorf("mgtt plan output missing 'healthy':\n%s", planOut)
	}

	// 7. Probe the image binary directly via docker run.
	// This validates the probe protocol without requiring a full model:
	//   docker run --rm <ref> probe my-widget count --type widget
	// should produce {"value":42,"raw":"42","status":"ok"}.
	// Use Stdout-only output so the JSON parse is not confused by docker stderr;
	// capture stderr separately to surface it on failure.
	probeCmd := exec.CommandContext(ctx, "docker", "run", "--rm", digestRef,
		"probe", "my-widget", "count", "--type", "widget",
	)
	probeOut, err := probeCmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("docker run probe: %v\nstderr: %s", err, stderr)
	}
	t.Logf("probe output: %s", probeOut)

	var probeResult struct {
		Value  interface{} `json:"value"`
		Raw    string      `json:"raw"`
		Status string      `json:"status"`
	}
	if err := json.Unmarshal(probeOut, &probeResult); err != nil {
		t.Fatalf("parse probe output %q: %v", probeOut, err)
	}
	if probeResult.Status != "ok" {
		t.Errorf("probe status: want 'ok', got %q", probeResult.Status)
	}
	// Value comes back as float64 from JSON unmarshal into interface{}.
	if v, ok := probeResult.Value.(float64); !ok || v != 42 {
		t.Errorf("probe value: want 42, got %v (%T)", probeResult.Value, probeResult.Value)
	}

	// 8. Uninstall.
	cmd = exec.CommandContext(ctx, mgttBin, "provider", "uninstall", "fixture")
	cmd.Env = append(os.Environ(), "MGTT_HOME="+mgttHome)
	cmd.Dir = repoRoot
	uninstallOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, uninstallOut)
	}
	t.Logf("uninstall output:\n%s", uninstallOut)

	// 8a. Provider directory must be gone.
	if _, err := os.Stat(providerDir); !os.IsNotExist(err) {
		t.Errorf("provider dir should be removed after uninstall; stat: %v", err)
	}

	// 8b. Uninstall output must contain "docker rmi" hint.
	if !strings.Contains(string(uninstallOut), "docker rmi") {
		t.Errorf("uninstall output missing 'docker rmi' hint:\n%s", uninstallOut)
	}
}
