# Build context: repo root (docker build -f test/integration/testdata/fixture-provider/Dockerfile .)
# The fixture go.mod has a replace directive pointing at ../../../../ (the repo root),
# so the build context must include both the fixture dir and the mgtt module.
FROM golang:1.25 AS build
WORKDIR /build

# Copy the mgtt module root so the replace directive can resolve it.
COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY sdk/ sdk/

# Copy the fixture provider source into a subdirectory.
COPY test/integration/testdata/fixture-provider/ fixture/

WORKDIR /build/fixture
# Repoint replace from host-relative path (../../../../) to image-relative path.
RUN go mod edit -replace=github.com/mgt-tool/mgtt=../
RUN go mod tidy
RUN go build -o /out/mgtt-provider-fixture .

FROM debian:12-slim
COPY --from=build /out/mgtt-provider-fixture /usr/local/bin/mgtt-provider-fixture
COPY test/integration/testdata/fixture-provider/provider.yaml /provider.yaml
ENTRYPOINT ["/usr/local/bin/mgtt-provider-fixture"]
