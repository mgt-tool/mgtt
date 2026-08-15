// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseOutput converts raw command output into a typed value according to the
// specified parse mode. The supported modes are:
//
//   - int          — trim whitespace, parse as integer
//   - float        — trim whitespace, parse as float64
//   - bool         — true/1/yes → true; false/0/no → false (case-insensitive)
//   - string       — trim whitespace, return as string
//   - exit_code    — exitCode==0 → true, else false (stdout ignored)
//   - json:<path>  — parse stdout as JSON, extract value at dot-path
//   - lines:<N>    — count non-empty lines (N is an unused legacy suffix)
//   - regex:<pat>  — first capture group, or whole match if no groups
func ParseOutput(mode string, stdout string, exitCode int) (any, error) {
	switch mode {
	case "int":
		return parseInt(stdout)
	case "float":
		return parseFloatMode(stdout)
	case "bool":
		return parseBoolMode(stdout)
	case "string":
		return strings.TrimSpace(stdout), nil
	case "exit_code":
		return exitCode == 0, nil
	case "age_seconds":
		return parseAgeSeconds(stdout)
	}
	// Prefixed modes carry an argument after the colon.
	switch {
	case strings.HasPrefix(mode, "json:"):
		return parseJSON(strings.TrimPrefix(mode, "json:"), stdout)
	case strings.HasPrefix(mode, "lines:"):
		return countNonEmptyLines(stdout), nil
	case strings.HasPrefix(mode, "regex:"):
		return parseRegex(strings.TrimPrefix(mode, "regex:"), stdout)
	}
	return nil, fmt.Errorf("unknown parse mode %q", mode)
}

func parseInt(stdout string) (any, error) {
	s := strings.TrimSpace(stdout)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("parse int: %w", err)
	}
	return v, nil
}

func parseFloatMode(stdout string) (any, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(stdout), 64)
	if err != nil {
		return nil, fmt.Errorf("parse float: %w", err)
	}
	return v, nil
}

func parseBoolMode(stdout string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(stdout)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return nil, fmt.Errorf("parse bool: unrecognised value %q", strings.TrimSpace(stdout))
	}
}

func parseAgeSeconds(stdout string) (any, error) {
	s := strings.TrimSpace(stdout)
	if s == "" {
		return 0, nil
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("parse age_seconds: %w", err)
	}
	return int(time.Since(ts).Seconds()), nil
}

// parseJSON extracts a value from JSON stdout using a dot-path expression.
// Supports ".field.nested", ".N" for array index, and "|length" for array length.
func parseJSON(path, stdout string) (any, error) {
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, fmt.Errorf("parse json: unmarshal: %w", err)
	}
	path, lengthMode := stripLengthSuffix(path)
	current, err := walkJSONPath(root, strings.TrimPrefix(path, "."))
	if err != nil {
		return nil, err
	}
	if lengthMode {
		arr, ok := current.([]any)
		if !ok {
			return nil, fmt.Errorf("parse json: |length requires array, got %T", current)
		}
		return len(arr), nil
	}
	return coerceJSONNumber(current), nil
}

// stripLengthSuffix pops "|length" off the end of a JSON path if
// present, returning the trimmed path and the mode flag.
func stripLengthSuffix(path string) (string, bool) {
	if strings.HasSuffix(path, "|length") {
		return strings.TrimSuffix(path, "|length"), true
	}
	return path, false
}

// walkJSONPath traverses dotted path through a decoded JSON value.
// Empty path returns root as-is. Integer segments index arrays;
// string segments look up map keys.
func walkJSONPath(root any, path string) (any, error) {
	if path == "" {
		return root, nil
	}
	current := root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		next, err := descendJSON(current, seg)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

// descendJSON returns current[seg] for maps and current[idx(seg)] for
// arrays. Other kinds (bool, number, string) are non-traversable.
func descendJSON(current any, seg string) (any, error) {
	switch node := current.(type) {
	case map[string]any:
		val, ok := node[seg]
		if !ok {
			return nil, fmt.Errorf("parse json: key %q not found", seg)
		}
		return val, nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return nil, fmt.Errorf("parse json: cannot index array with %q", seg)
		}
		if idx < 0 || idx >= len(node) {
			return nil, fmt.Errorf("parse json: array index %d out of bounds (len=%d)", idx, len(node))
		}
		return node[idx], nil
	}
	return nil, fmt.Errorf("parse json: cannot traverse %T with key %q", current, seg)
}

// coerceJSONNumber turns whole-number float64 values back into int,
// since encoding/json decodes every number as float64.
func coerceJSONNumber(v any) any {
	f, ok := v.(float64)
	if !ok {
		return v
	}
	if f == float64(int(f)) {
		return int(f)
	}
	return f
}

// countNonEmptyLines counts lines in stdout that are non-empty after trimming.
func countNonEmptyLines(stdout string) int {
	count := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// parseRegex applies the pattern to stdout. If the pattern has capture groups,
// it returns the first group. Otherwise it returns the full match.
func parseRegex(pattern, stdout string) (any, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse regex: compile %q: %w", pattern, err)
	}

	// Match against trimmed stdout so trailing newlines don't interfere.
	s := strings.TrimSpace(stdout)
	match := re.FindStringSubmatch(s)
	if match == nil {
		return nil, fmt.Errorf("parse regex: pattern %q did not match %q", pattern, s)
	}

	if len(match) > 1 {
		// Return first capture group.
		return match[1], nil
	}
	// No capture groups — return whole match.
	return match[0], nil
}
