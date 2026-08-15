package main

import (
	"context"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

func main() {
	r := provider.NewRegistry()
	r.Register("widget", map[string]provider.ProbeFn{
		"count": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.IntResult(42), nil
		},
	})
	provider.Main(r)
}
