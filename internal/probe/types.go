package probe

import (
	"context"
	"time"
)

// Executor runs a probe command and returns the result.
type Executor interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// Command describes a single probe to execute.
type Command struct {
	Raw       string        // fully substituted command string
	Parse     string        // parse mode: "int", "float", "bool", "string", "exit_code", "json:<path>", "lines:<N>", "regex:<pat>"
	Provider  string        // for logging
	Component string        // for logging
	Fact      string        // for logging
	Timeout   time.Duration // 0 = default 30s
}

// Result holds the output of a probe execution.
type Result struct {
	Raw      string // original stdout
	Parsed   any    // typed value after parsing
	ExitCode int
}
