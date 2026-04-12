package cli

import "fmt"

func checkmark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
