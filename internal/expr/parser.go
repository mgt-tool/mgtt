package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Parse tokenizes input and builds an AST from the expression grammar:
//
//	expr    = or
//	or      = and ("|" and)*
//	and     = primary ("&" primary)*
//	primary = "(" expr ")" | ref cmp value
func Parse(input string) (Node, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.tokens[p.pos], p.pos)
	}
	return node, nil
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

// tokenize splits input into tokens, keeping operators as separate tokens.
// Whitespace is a separator. Multi-character operators (==, !=, <=, >=) are
// kept whole. component.fact is kept as a single token.
func tokenize(input string) ([]string, error) {
	var tokens []string
	i := 0
	runes := []rune(input)
	n := len(runes)
	for i < n {
		ch := runes[i]

		// Skip whitespace.
		if unicode.IsSpace(ch) {
			i++
			continue
		}

		// Single-char tokens that could be two-char operators.
		if ch == '=' || ch == '!' || ch == '<' || ch == '>' {
			if i+1 < n && runes[i+1] == '=' {
				tokens = append(tokens, string(runes[i:i+2]))
				i += 2
			} else {
				tokens = append(tokens, string(ch))
				i++
			}
			continue
		}

		// Simple single-char tokens.
		if ch == '&' || ch == '|' || ch == '(' || ch == ')' {
			tokens = append(tokens, string(ch))
			i++
			continue
		}

		// Identifier or number (including component.fact and floats with '.').
		// Read a "word": letters, digits, underscore, dot, minus.
		if isWordStart(ch) || ch == '-' {
			j := i
			for j < n && isWordContinue(runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
			continue
		}

		return nil, fmt.Errorf("unexpected character %q", ch)
	}
	return tokens, nil
}

func isWordStart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isWordContinue(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-'
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type parser struct {
	tokens []string
	pos    int
}

func (p *parser) peek() (string, bool) {
	if p.pos >= len(p.tokens) {
		return "", false
	}
	return p.tokens[p.pos], true
}

func (p *parser) consume() string {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) expect(tok string) error {
	got, ok := p.peek()
	if !ok {
		return fmt.Errorf("expected %q but reached end of input", tok)
	}
	if got != tok {
		return fmt.Errorf("expected %q got %q", tok, got)
	}
	p.consume()
	return nil
}

// parseOr handles the lowest-precedence | operator.
func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok != "|" {
			break
		}
		p.consume()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &OrNode{L: left, R: right}
	}
	return left, nil
}

// parseAnd handles the & operator.
func (p *parser) parseAnd() (Node, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		tok, ok := p.peek()
		if !ok || tok != "&" {
			break
		}
		p.consume()
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = &AndNode{L: left, R: right}
	}
	return left, nil
}

// parsePrimary handles parenthesised sub-expressions and leaf comparisons.
func (p *parser) parsePrimary() (Node, error) {
	tok, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("unexpected end of input")
	}

	if tok == "(" {
		p.consume()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return inner, nil
	}

	// ref cmp value
	return p.parseCmp()
}

// parseCmp parses:  ref cmp value
func (p *parser) parseCmp() (Node, error) {
	ref, err := p.parseRef()
	if err != nil {
		return nil, err
	}

	op, err := p.parseCmpOp()
	if err != nil {
		return nil, err
	}

	val, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	return &CmpNode{
		Component: ref.component,
		Fact:      ref.fact,
		Op:        op,
		Value:     val,
	}, nil
}

type ref struct {
	component string
	fact      string
}

// parseRef reads a reference token (e.g. "ready_replicas" or "api.ready_replicas").
func (p *parser) parseRef() (ref, error) {
	tok, ok := p.peek()
	if !ok {
		return ref{}, fmt.Errorf("expected ref, got end of input")
	}
	// Reject tokens that look like operators or values.
	if isCmpOp(tok) || tok == "&" || tok == "|" || tok == "(" || tok == ")" {
		return ref{}, fmt.Errorf("expected ref token, got operator %q", tok)
	}
	p.consume()

	if idx := strings.IndexByte(tok, '.'); idx >= 0 {
		return ref{component: tok[:idx], fact: tok[idx+1:]}, nil
	}
	return ref{fact: tok}, nil
}

// parseCmpOp reads a comparison operator.
func (p *parser) parseCmpOp() (CmpOp, error) {
	tok, ok := p.peek()
	if !ok {
		return 0, fmt.Errorf("expected comparison operator, got end of input")
	}
	var op CmpOp
	switch tok {
	case "==":
		op = OpEq
	case "!=":
		op = OpNeq
	case "<":
		op = OpLt
	case ">":
		op = OpGt
	case "<=":
		op = OpLte
	case ">=":
		op = OpGte
	default:
		return 0, fmt.Errorf("expected comparison operator, got %q", tok)
	}
	p.consume()
	return op, nil
}

// isCmpOp reports whether s is a comparison operator.
func isCmpOp(s string) bool {
	switch s {
	case "==", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// parseValue reads an int, float, bool, or string literal.
func (p *parser) parseValue() (Value, error) {
	tok, ok := p.peek()
	if !ok {
		return Value{}, fmt.Errorf("expected value, got end of input")
	}
	p.consume()
	return parseValueToken(tok), nil
}

// parseValueToken converts a token string into a typed Value.
func parseValueToken(tok string) Value {
	// bool
	if tok == "true" {
		b := true
		return Value{BoolVal: &b}
	}
	if tok == "false" {
		b := false
		return Value{BoolVal: &b}
	}
	// int
	if iv, err := strconv.Atoi(tok); err == nil {
		return Value{IntVal: &iv}
	}
	// float
	if fv, err := strconv.ParseFloat(tok, 64); err == nil {
		return Value{FloatVal: &fv}
	}
	// string
	s := tok
	return Value{StringVal: &s}
}
