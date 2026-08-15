VERSION := $(shell cat VERSION)
LDFLAGS := -ldflags "-X github.com/mgt-tool/mgtt/internal/cli.version=$(VERSION)"

.PHONY: build test vet clean

build:
	go build $(LDFLAGS) -o mgtt ./cmd/mgtt

test:
	go vet ./...
	go test ./...

clean:
	rm -f mgtt

docker:
	docker compose build --build-arg VERSION=$(VERSION) mgtt
