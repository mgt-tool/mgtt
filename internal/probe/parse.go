package probe

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
//   - lines:<N>    — count non-empty lines (N is ignored in this version)
//   - regex:<pat>  — first capture group, or whole match if no groups
func ParseOutput(mode string, stdout string, exitCode int) (any, error) {
	switch {
	case mode == "int":
		s := strings.TrimSpace(stdout)
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("parse int: %w", err)
		}
		return v, nil

	case mode == "float":
		s := strings.TrimSpace(stdout)
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("parse float: %w", err)
		}
		return v, nil

	case mode == "bool":
		s := strings.ToLower(strings.TrimSpace(stdout))
		switch s {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		default:
			return nil, fmt.Errorf("parse bool: unrecognised value %q", s)
		}

	case mode == "string":
		return strings.TrimSpace(stdout), nil

	case mode == "exit_code":
		return exitCode == 0, nil

	case strings.HasPrefix(mode, "json:"):
		path := strings.TrimPrefix(mode, "json:")
		return parseJSON(path, stdout)

	case strings.HasPrefix(mode, "lines:"):
		return countNonEmptyLines(stdout), nil

	case strings.HasPrefix(mode, "regex:"):
		pattern := strings.TrimPrefix(mode, "regex:")
		return parseRegex(pattern, stdout)

	default:
		return nil, fmt.Errorf("unknown parse mode %q", mode)
	}
}

// parseJSON extracts a value from JSON stdout using a dot-path expression.
// Supports ".field.nested", ".N" for array index, and "|length" for array length.
func parseJSON(path, stdout string) (any, error) {
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, fmt.Errorf("parse json: unmarshal: %w", err)
	}

	// Handle |length suffix.
	lengthMode := false
	if strings.HasSuffix(path, "|length") {
		lengthMode = true
		path = strings.TrimSuffix(path, "|length")
	}

	// Strip leading dot.
	path = strings.TrimPrefix(path, ".")

	current := root
	if path != "" {
		segments := strings.Split(path, ".")
		for _, seg := range segments {
			if seg == "" {
				continue
			}
			switch node := current.(type) {
			case map[string]any:
				val, ok := node[seg]
				if !ok {
					return nil, fmt.Errorf("parse json: key %q not found", seg)
				}
				current = val
			case []any:
				idx, err := strconv.Atoi(seg)
				if err != nil {
					return nil, fmt.Errorf("parse json: cannot index array with %q", seg)
				}
				if idx < 0 || idx >= len(node) {
					return nil, fmt.Errorf("parse json: array index %d out of bounds (len=%d)", idx, len(node))
				}
				current = node[idx]
			default:
				return nil, fmt.Errorf("parse json: cannot traverse %T with key %q", current, seg)
			}
		}
	}

	if lengthMode {
		arr, ok := current.([]any)
		if !ok {
			return nil, fmt.Errorf("parse json: |length requires array, got %T", current)
		}
		return len(arr), nil
	}

	// Coerce JSON numbers to int when they are whole numbers.
	if f, ok := current.(float64); ok {
		if f == float64(int(f)) {
			return int(f), nil
		}
		return f, nil
	}

	return current, nil
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
