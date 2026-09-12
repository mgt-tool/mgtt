# mgtt, from the command line.
#
#   make              build ./mgtt, stamped with VERSION
#   make test         vet and the suites
#   make check        the same, plus: the version is one the changelog knows
#   make dist         every release platform into dist/, with SHA256SUMS
#   make docker       the image, tagged mgtt:<version>
#   make tag          tag v<VERSION> and push it, which releases it
#   make clean
#
# One source for the version: the VERSION file.  The binary prints it as
# vX.Y.Z -- the spelling the Go proxy and a git tag use -- so that a binary
# from `go install`, from the image and from a release asset all answer
# `mgtt version` the same way.

VERSION   := $(shell tr -d '[:space:]' < VERSION)
TAG       := v$(VERSION)
LDFLAGS   := -s -w -X github.com/mgt-tool/mgtt/internal/cli.version=$(TAG)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build test check dist docker tag clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o mgtt ./cmd/mgtt

test:
	go vet ./...
	go test ./...

check: test
	sh scripts/release-check.sh

# One binary per platform, named the way install.sh looks for them, and one
# checksum file covering all of them.  CI builds this on every push so that
# a tag is never the first time a platform fails to compile.
dist:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  echo "  $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
	    go build -trimpath -ldflags "$(LDFLAGS)" -o dist/mgtt-$$os-$$arch ./cmd/mgtt || exit 1; \
	done
	@cd dist && sha256sum mgtt-* > SHA256SUMS
	@echo "dist/: $(TAG), $(words $(PLATFORMS)) platforms"

docker:
	docker build --build-arg VERSION=$(TAG) -t mgtt:$(VERSION) .

# A release is a tag; everything else follows from it in CI.  Refuses unless
# the tree agrees with itself about the version -- see scripts/release-check.sh.
tag:
	sh scripts/release-check.sh $(TAG)
	git diff --quiet && git diff --cached --quiet || { echo "commit first" >&2; exit 1; }
	git tag -a $(TAG) -m "mgtt $(TAG)"
	git push origin $(TAG)
	@echo "tagged $(TAG); the release workflow takes it from here"

clean:
	rm -rf mgtt dist
