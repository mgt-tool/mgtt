package model

import (
	"testing"
)

func TestSemVer_Parse(t *testing.T) {
	cases := []struct {
		input   string
		want    semVersion
		wantErr bool
	}{
		{"1.2.3", semVersion{1, 2, 3}, false},
		{"v1.2.3", semVersion{1, 2, 3}, false},
		{"0.0.0", semVersion{0, 0, 0}, false},
		{"0.2.0", semVersion{0, 2, 0}, false},
		{"10.20.30", semVersion{10, 20, 30}, false},
		// Partial versions (missing patch)
		{"0.2", semVersion{0, 2, 0}, false},
		{"1", semVersion{1, 0, 0}, false},
		// v prefix
		{"v0.5.1", semVersion{0, 5, 1}, false},
		// Pre-release stripped
		{"1.2.3-alpha", semVersion{1, 2, 3}, false},
		// Build metadata stripped
		{"1.2.3+build", semVersion{1, 2, 3}, false},
		// Errors
		{"", semVersion{}, true},
		{"abc", semVersion{}, true},
		{"1.x.3", semVersion{}, true},
		{"-1.2.3", semVersion{}, true},
	}
	for _, tc := range cases {
		got, err := parseSemVer(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseSemVer(%q): err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got != tc.want {
			t.Errorf("parseSemVer(%q): got %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestSemVer_Compare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"0.2.5", "0.3.0", -1},
		{"0.2.0", "0.2.0", 0},
		{"0.0.1", "0.0.2", -1},
	}
	for _, tc := range cases {
		a, _ := parseSemVer(tc.a)
		b, _ := parseSemVer(tc.b)
		got := a.compare(b)
		if got != tc.want {
			t.Errorf("(%s).compare(%s): got %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSemVer_Satisfies(t *testing.T) {
	cases := []struct {
		version    string
		constraint string
		want       bool
		wantErr    bool
	}{
		// Empty constraint — any version matches
		{"1.2.3", "", true, false},
		{"0.0.1", "", true, false},
		// Exact match
		{"0.2.0", "0.2.0", true, false},
		{"0.2.1", "0.2.0", false, false},
		// >=
		{"0.5.0", ">=0.5.0", true, false},
		{"0.5.1", ">=0.5.0", true, false},
		{"0.4.9", ">=0.5.0", false, false},
		// <
		{"0.9.9", "<1.0.0", true, false},
		{"1.0.0", "<1.0.0", false, false},
		{"1.0.1", "<1.0.0", false, false},
		// >
		{"1.0.1", ">1.0.0", true, false},
		{"1.0.0", ">1.0.0", false, false},
		// <=
		{"1.0.0", "<=1.0.0", true, false},
		{"0.9.9", "<=1.0.0", true, false},
		{"1.0.1", "<=1.0.0", false, false},
		// Range (comma-separated)
		{"0.5.0", ">=0.5.0,<1.0.0", true, false},
		{"0.9.9", ">=0.5.0,<1.0.0", true, false},
		{"0.4.9", ">=0.5.0,<1.0.0", false, false},
		{"1.0.0", ">=0.5.0,<1.0.0", false, false},
		// Caret — major=0: next minor boundary
		{"0.2.0", "^0.2", true, false},
		{"0.2.5", "^0.2", true, false},
		{"0.3.0", "^0.2", false, false},
		{"0.1.9", "^0.2", false, false},
		// Caret — major>=1: next major boundary
		{"1.2.0", "^1.2", true, false},
		{"1.9.9", "^1.2", true, false},
		{"2.0.0", "^1.2", false, false},
		{"1.1.9", "^1.2", false, false},
		// v-prefixed version string
		{"v0.2.0", "0.2.0", true, false},
		// Unparseable constraint → error
		{"1.0.0", ">=notaversion", false, true},
	}
	for _, tc := range cases {
		v, err := parseSemVer(tc.version)
		if err != nil {
			t.Fatalf("parseSemVer(%q): %v", tc.version, err)
		}
		got, err := v.satisfies(tc.constraint)
		if (err != nil) != tc.wantErr {
			t.Errorf("(%s).satisfies(%q): err=%v, wantErr=%v", tc.version, tc.constraint, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got != tc.want {
			t.Errorf("(%s).satisfies(%q): got %v, want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
}
