package providersupport

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateImageRef(t *testing.T) {
	cases := map[string]bool{
		"":                               false,
		"ghcr.io/foo/bar":                false, // no tag, no digest
		"ghcr.io/foo/bar:tag":            false, // tag without digest — silently rolls
		"ghcr.io/foo/bar:tag@sha256:abc": true,
		"ghcr.io/foo/bar@sha256:abc":     true,
		"foo/bar@sha256:abc":             true,
	}
	for ref, ok := range cases {
		err := ValidateImageRef(ref)
		got := err == nil
		if got != ok {
			t.Errorf("ValidateImageRef(%q): want ok=%v, got err=%v", ref, ok, err)
		}
	}
}

// tarBytes builds an in-memory tar archive containing a single file entry
// with the given name and body. docker cp to stdout emits exactly this shape.
func tarBytes(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractManifest_HappyPath verifies the create → cp → rm sequence and
// that the tar stream from `docker cp` is decoded back to the file bytes.
// The old implementation shelled out to `docker run --entrypoint cat`, which
// required the image to include `cat`; that excluded distroless/scratch bases.
func TestExtractManifest_HappyPath(t *testing.T) {
	manifest := []byte("meta:\n  name: foo\n")
	var calls []string
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			calls = append(calls, joined)
			switch args[0] {
			case "create":
				return []byte("container-abc\n"), nil
			case "cp":
				if !strings.Contains(joined, "container-abc:/manifest.yaml") {
					t.Errorf("cp: expected container-abc:/manifest.yaml, got %s", joined)
				}
				return tarBytes(t, "manifest.yaml", manifest), nil
			case "rm":
				if !strings.Contains(joined, "container-abc") {
					t.Errorf("rm: expected container-abc in args, got %s", joined)
				}
				return nil, nil
			default:
				t.Fatalf("unexpected docker subcommand: %s", joined)
				return nil, nil
			}
		},
	}
	body, err := d.ExtractManifest(context.Background(), "ghcr.io/x@sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, manifest) {
		t.Fatalf("unexpected body: %q", body)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 docker calls (create, cp, rm), got %d: %v", len(calls), calls)
	}
	if !strings.HasPrefix(calls[0], "create ") {
		t.Errorf("call[0] should be create, got %q", calls[0])
	}
	if !strings.HasPrefix(calls[1], "cp ") {
		t.Errorf("call[1] should be cp, got %q", calls[1])
	}
	if !strings.HasPrefix(calls[2], "rm ") {
		t.Errorf("call[2] should be rm, got %q", calls[2])
	}
}

// TestExtractManifest_MissingFile verifies that a cp failure (the file isn't
// in the image) wraps the underlying docker error and mentions /manifest.yaml.
// The container must still be removed.
func TestExtractManifest_MissingFile(t *testing.T) {
	underlyingErr := errors.New("Error: No such container:path")
	rmCalled := false
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "create":
				return []byte("container-abc\n"), nil
			case "cp":
				return nil, underlyingErr
			case "rm":
				rmCalled = true
				return nil, nil
			}
			return nil, nil
		},
	}
	_, err := d.ExtractManifest(context.Background(), "ghcr.io/x@sha256:abc")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, underlyingErr) {
		t.Fatalf("expected error wrapping underlying docker error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/manifest.yaml") {
		t.Fatalf("expected error message mentioning /manifest.yaml, got %v", err)
	}
	if !rmCalled {
		t.Fatalf("expected rm to be called for cleanup even when cp fails")
	}
}

func TestExtractManifest_CreateFails(t *testing.T) {
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			if args[0] == "create" {
				return []byte("manifest unknown"), errors.New("pull access denied")
			}
			t.Fatalf("no other docker call expected when create fails, got %v", args)
			return nil, nil
		},
	}
	_, err := d.ExtractManifest(context.Background(), "ghcr.io/x@sha256:abc")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "docker create") {
		t.Fatalf("expected error to mention docker create, got %v", err)
	}
}

// tarDirBytes builds a tar archive shaped like `docker cp cid:/types -`:
// a top-level directory entry followed by one regular-file entry per member.
func tarDirBytes(t *testing.T, dirName string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     dirName + "/",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     dirName + "/" + name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractTypes_HappyPath verifies that multi-file providers' /types/
// directory is extracted alongside /manifest.yaml. Kubernetes, tempo,
// quickwit, and terraform all ship types/<name>.yaml layouts; without
// this, image-install would drop every type on the floor.
func TestExtractTypes_HappyPath(t *testing.T) {
	typeFiles := map[string]string{
		"deployment.yaml": "description: k8s deployment\n",
		"service.yaml":    "description: k8s service\n",
	}
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "create":
				return []byte("cid-abc\n"), nil
			case "cp":
				if !strings.Contains(strings.Join(args, " "), "cid-abc:/types") {
					t.Errorf("cp: expected cid-abc:/types, got %v", args)
				}
				return tarDirBytes(t, "types", typeFiles), nil
			case "rm":
				return nil, nil
			}
			return nil, nil
		},
	}
	got, err := d.ExtractTypes(context.Background(), "ghcr.io/x@sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(typeFiles) {
		t.Fatalf("want %d types, got %d", len(typeFiles), len(got))
	}
	for name, want := range typeFiles {
		if string(got[name]) != want {
			t.Errorf("type %q: want %q, got %q", name, want, got[name])
		}
	}
}

// TestExtractTypes_MissingDirIsNotAnError exercises providers that use
// inline `types:` in manifest.yaml and have no types/ directory. docker cp
// fails with "No such file or directory"; we must return (nil, nil) not an
// error — the caller treats absence as "nothing to copy".
func TestExtractTypes_MissingDirIsNotAnError(t *testing.T) {
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "create":
				return []byte("cid-abc\n"), nil
			case "cp":
				return []byte("Error: No such container:path: cid-abc:/types"), errors.New("exit status 1")
			case "rm":
				return nil, nil
			}
			return nil, nil
		},
	}
	got, err := d.ExtractTypes(context.Background(), "ghcr.io/x@sha256:abc")
	if err != nil {
		t.Fatalf("missing /types must not error; got %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("missing /types must yield no types; got %d", len(got))
	}
}

func TestPullImage_PassesRefThrough(t *testing.T) {
	var seen string
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			seen = strings.Join(args, " ")
			return nil, nil
		},
	}
	if err := d.PullImage(context.Background(), "ghcr.io/x@sha256:abc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "pull ghcr.io/x@sha256:abc") {
		t.Fatalf("expected `docker pull <ref>`, saw %q", seen)
	}
}
