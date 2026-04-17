package providersupport

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DockerCmd is the small surface mgtt needs to install providers from
// Docker images. Tests inject Run; production uses the real `docker` CLI.
type DockerCmd struct {
	Run     func(ctx context.Context, args ...string) ([]byte, error)
	Timeout time.Duration // Timeout bounds a single docker invocation. Zero means no timeout.
}

// NewDockerCmd returns a DockerCmd that shells out to the host `docker`.
func NewDockerCmd() *DockerCmd {
	return &DockerCmd{
		Timeout: 5 * time.Minute,
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		},
	}
}

// ValidateImageRef rejects refs that aren't pinned by digest. Mgtt's whole
// point of supporting image install is reproducibility — bare tags can be
// re-rolled (the lesson from grafana/tempo:2.6.0). The check is structural,
// not semantic: the ref must contain "@sha256:".
func ValidateImageRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("image ref is required")
	}
	if !strings.Contains(ref, "@sha256:") {
		return fmt.Errorf("image ref must include @sha256: digest for reproducibility (got %q)", ref)
	}
	return nil
}

// PullImage runs `docker pull <ref>`. Returns the docker output on error so
// callers can show the user what went wrong.
func (d *DockerCmd) PullImage(ctx context.Context, ref string) error {
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}
	out, err := d.Run(ctx, "pull", ref)
	if err != nil {
		return fmt.Errorf("docker pull %s: %w\n%s", ref, err, out)
	}
	return nil
}

// ExtractManifest creates a container from the image, copies /manifest.yaml
// out of its filesystem, then removes the container. Works for any base
// image — including distroless and scratch — because nothing inside the
// container is executed. The provider image MUST embed its manifest.yaml
// at /manifest.yaml; no other location is supported.
//
// `docker cp <cid>:/manifest.yaml -` emits a tar archive on stdout containing
// the single file; we decode the first regular-file entry and return its body.
func (d *DockerCmd) ExtractManifest(ctx context.Context, ref string) ([]byte, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	cidOut, err := d.Run(ctx, "create", ref)
	if err != nil {
		return nil, fmt.Errorf("docker create %s: %w\n%s", ref, err, cidOut)
	}
	cid := strings.TrimSpace(string(cidOut))
	if cid == "" {
		return nil, fmt.Errorf("docker create %s: empty container id", ref)
	}
	defer func() {
		_, _ = d.Run(ctx, "rm", "-v", cid)
	}()

	tarOut, err := d.Run(ctx, "cp", cid+":/manifest.yaml", "-")
	if err != nil {
		return nil, fmt.Errorf("docker cp /manifest.yaml from %s: %w\n%s", ref, err, tarOut)
	}

	tr := tar.NewReader(bytes.NewReader(tarOut))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("extract /manifest.yaml from %s: no regular file in tar stream", ref)
		}
		if err != nil {
			return nil, fmt.Errorf("extract /manifest.yaml from %s: read tar header: %w", ref, err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck // TypeRegA is legacy but still appears
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract /manifest.yaml from %s: read body: %w", ref, err)
		}
		return body, nil
	}
}

// ExtractTypes pulls the /types/ directory out of the image as a map of
// filename → contents. Multi-file providers (kubernetes, tempo, quickwit,
// terraform) declare one type per file under types/; without this step the
// install directory would only contain manifest.yaml and every type would
// disappear on image install.
//
// Returns (nil, nil) when the image has no /types/ directory — inline-types
// providers are valid and their absence is not an error.
func (d *DockerCmd) ExtractTypes(ctx context.Context, ref string) (map[string][]byte, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	cidOut, err := d.Run(ctx, "create", ref)
	if err != nil {
		return nil, fmt.Errorf("docker create %s: %w\n%s", ref, err, cidOut)
	}
	cid := strings.TrimSpace(string(cidOut))
	if cid == "" {
		return nil, fmt.Errorf("docker create %s: empty container id", ref)
	}
	defer func() {
		_, _ = d.Run(ctx, "rm", "-v", cid)
	}()

	tarOut, err := d.Run(ctx, "cp", cid+":/types", "-")
	if err != nil {
		// `docker cp` errors on missing source. Accept absence silently so
		// inline-types providers still install cleanly.
		return nil, nil
	}

	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(tarOut))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("extract /types from %s: read tar header: %w", ref, err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck // TypeRegA is legacy
			continue
		}
		// Tar entries look like "types/deployment.yaml" — strip the leading dir.
		base := filepath.Base(hdr.Name)
		if filepath.Ext(base) != ".yaml" {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", hdr.Name, err)
		}
		out[base] = body
	}
	return out, nil
}
