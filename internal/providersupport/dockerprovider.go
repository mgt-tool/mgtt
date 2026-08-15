// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
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

// isDockerCpSourceAbsent recognises the stderr fragments Docker emits when
// the copy source does not exist inside the container — across both the
// current "No such container:path" daemon form and older CLI variants.
// Only these strings demote the error to "absent source"; any other
// failure (daemon down, permission denied) is surfaced.
func isDockerCpSourceAbsent(msg string) bool {
	for _, marker := range []string{
		"No such container:path",
		"No such file or directory",
		"Could not find the file",
		"not found in container",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
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

// withContainer creates a short-lived container from ref, invokes fn
// with its id, and removes the container on return. A cleanup failure
// is logged rather than swallowed (previous behaviour was `_, _ =
// d.Run(...)`, which hid daemon outages).
//
// The cleanup runs on a fresh context: if fn exhausts d.Timeout or the
// parent ctx is cancelled, the `docker rm` must still have its own
// budget or it errors out immediately and orphans the container.
func (d *DockerCmd) withContainer(ctx context.Context, ref string, fn func(cid string) error) error {
	workCtx := ctx
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}
	cidOut, err := d.Run(workCtx, "create", ref)
	if err != nil {
		return fmt.Errorf("docker create %s: %w\n%s", ref, err, cidOut)
	}
	cid := strings.TrimSpace(string(cidOut))
	if cid == "" {
		return fmt.Errorf("docker create %s: empty container id", ref)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if out, err := d.Run(cleanupCtx, "rm", "-v", cid); err != nil {
			log.Printf("docker rm %s: %v\n%s", cid, err, out)
		}
	}()
	return fn(cid)
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
	var out []byte
	err := d.withContainer(ctx, ref, func(cid string) error {
		body, err := d.extractManifestBody(ctx, ref, cid)
		out = body
		return err
	})
	return out, err
}

func (d *DockerCmd) extractManifestBody(ctx context.Context, ref, cid string) ([]byte, error) {
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
		if hdr.Typeflag != tar.TypeReg {
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
	var out map[string][]byte
	err := d.withContainer(ctx, ref, func(cid string) error {
		res, err := d.extractTypesBody(ctx, ref, cid)
		out = res
		return err
	})
	return out, err
}

func (d *DockerCmd) extractTypesBody(ctx context.Context, ref, cid string) (map[string][]byte, error) {
	tarOut, err := d.Run(ctx, "cp", cid+":/types", "-")
	if err != nil {
		// `docker cp` exits non-zero for every failure; distinguish
		// "source absent" (inline-types providers, fine to swallow) from
		// real failures (daemon down, permission denied) by matching the
		// stderr fragment Docker emits for missing paths. Anything that
		// doesn't match is surfaced so the operator sees the real error
		// instead of a silent types-less install.
		if isDockerCpSourceAbsent(string(tarOut)) {
			return nil, nil
		}
		return nil, fmt.Errorf("docker cp /types from %s: %w\n%s", ref, err, tarOut)
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
		if hdr.Typeflag != tar.TypeReg {
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
