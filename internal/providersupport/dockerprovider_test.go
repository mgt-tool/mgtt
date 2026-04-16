package providersupport

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateImageRef(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		"ghcr.io/foo/bar":                     false, // no tag, no digest
		"ghcr.io/foo/bar:tag":                 false, // tag without digest — silently rolls
		"ghcr.io/foo/bar:tag@sha256:abc":      true,
		"ghcr.io/foo/bar@sha256:abc":          true,
		"foo/bar@sha256:abc":                  true,
	}
	for ref, ok := range cases {
		err := ValidateImageRef(ref)
		got := err == nil
		if got != ok {
			t.Errorf("ValidateImageRef(%q): want ok=%v, got err=%v", ref, ok, err)
		}
	}
}

func TestExtractManifest_HappyPath(t *testing.T) {
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			// Expect: docker run --rm --entrypoint cat <ref> /provider.yaml
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--entrypoint cat") || !strings.Contains(joined, "/provider.yaml") {
				t.Errorf("unexpected args: %s", joined)
			}
			return []byte("meta:\n  name: foo\n"), nil
		},
	}
	body, err := d.ExtractManifest(context.Background(), "ghcr.io/x@sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "meta:\n  name: foo\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestExtractManifest_MissingFile(t *testing.T) {
	underlyingErr := errors.New("cat: /provider.yaml: No such file or directory")
	d := &DockerCmd{
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			return nil, underlyingErr
		},
	}
	_, err := d.ExtractManifest(context.Background(), "ghcr.io/x@sha256:abc")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, underlyingErr) {
		t.Fatalf("expected error wrapping underlying docker error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/provider.yaml") {
		t.Fatalf("expected error message mentioning /provider.yaml, got %v", err)
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
