package pii

import (
	"fmt"
	"regexp"
	"strings"
)

// placeholderPattern matches any placeholder token this package could have
// generated, e.g. "[EMAIL_1]", "[SIRET_2]", "[EMPLOYEE_ID_3]" for a custom
// rule.
var placeholderPattern = regexp.MustCompile(`\[[A-Z][A-Z0-9_]*_\d+\]`)

// Table is a request-scoped mapping between original sensitive values and
// the placeholders that replace them, so a response can later be restored
// before it reaches the caller. A Table must not be reused across
// requests: it exists only for the lifetime of one proxied call.
type Table struct {
	counts  map[string]int
	forward map[string]string // placeholder -> original value
	reverse map[string]string // original value -> placeholder (dedupe)
}

// NewTable creates an empty, request-scoped re-identification table.
func NewTable() *Table {
	return &Table{
		counts:  map[string]int{},
		forward: map[string]string{},
		reverse: map[string]string{},
	}
}

// placeholderFor returns the placeholder for value, reusing the same
// placeholder if this exact value was already masked earlier in the same
// request (e.g. the same email repeated twice in one prompt).
func (t *Table) placeholderFor(typ, value string) string {
	if ph, ok := t.reverse[value]; ok {
		return ph
	}
	t.counts[typ]++
	ph := fmt.Sprintf("[%s_%d]", typ, t.counts[typ])
	t.forward[ph] = value
	t.reverse[value] = ph
	return ph
}

// Mask returns text with every match replaced by its placeholder.
func (t *Table) Mask(text string, matches []Match) string {
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(text[last:m.Start])
		b.WriteString(t.placeholderFor(m.Type, m.Value))
		last = m.End
	}
	b.WriteString(text[last:])
	return b.String()
}

// Len reports how many distinct values have been masked so far.
func (t *Table) Len() int { return len(t.forward) }

// Unmask returns text with every known placeholder token replaced by its
// original value. A placeholder-shaped token this table never produced is
// left untouched.
func (t *Table) Unmask(text string) string {
	if len(t.forward) == 0 {
		return text
	}
	return placeholderPattern.ReplaceAllStringFunc(text, func(ph string) string {
		if orig, ok := t.forward[ph]; ok {
			return orig
		}
		return ph
	})
}

// Lookup returns the original value for a single placeholder token, if
// this table produced it.
func (t *Table) Lookup(placeholder string) (string, bool) {
	v, ok := t.forward[placeholder]
	return v, ok
}
