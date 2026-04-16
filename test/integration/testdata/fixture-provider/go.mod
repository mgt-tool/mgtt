module mgtt-provider-fixture

go 1.25.7

require github.com/mgt-tool/mgtt v0.1.4

// Replace patched at image build; host `go build` uses the `../../../../` path,
// Docker build rewrites this to `../` via `go mod edit` in the Dockerfile.
replace github.com/mgt-tool/mgtt => ../../../../
