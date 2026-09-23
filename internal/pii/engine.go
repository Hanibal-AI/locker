// Package pii implements the PII detection and anonymization pipeline —
// Layer 1 (RegEx + validation formulas) as described in Docs/roadmap.md
// Phase 2. Local NER (Phase 3) and the symbolic "Fourmi" layer (Phase 4)
// build on top of this package's Engine/Table.
package pii

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Hanibal-AI/locker/internal/config"
	"github.com/Hanibal-AI/locker/internal/pii/ner"
)

// Match is a single detected PII occurrence in a piece of text.
type Match struct {
	Type  string
	Value string
	Start int
	End   int
}

// Engine detects PII in text using Layer 1 (a set of RegEx rules, each
// optionally backed by a validation formula such as Luhn or an IBAN
// checksum) and, unless disabled, Layer 2 (internal/pii/ner, for
// unstructured entities RegEx cannot catch: names, organizations,
// locations, dates).
type Engine struct {
	rules []rule
	ner   ner.Recognizer
}

// NewEngine builds an Engine from configuration.
//
//   - If cfg.Disabled is true, the Engine detects nothing (Detect always
//     returns no matches) — this is how PII masking is turned off.
//   - If cfg.EnabledRules is empty, all built-in rules are enabled.
//     Otherwise only the named ones are (see builtinRuleName for the
//     accepted names: "email", "phone", "iban", "credit_card", "siren",
//     "siret").
//   - cfg.CustomRules are always added on top, matched by shape only (no
//     validation formula), keyed by their own placeholder/name.
func NewEngine(cfg config.PIIConfig) (*Engine, error) {
	if cfg.Disabled {
		return &Engine{}, nil
	}

	enabled := make(map[string]bool, len(cfg.EnabledRules))
	for _, name := range cfg.EnabledRules {
		enabled[name] = true
	}
	allBuiltins := len(cfg.EnabledRules) == 0

	var rules []rule
	for _, r := range builtinRules() {
		if allBuiltins || enabled[builtinRuleName(r.typ)] {
			rules = append(rules, r)
		}
	}

	for _, c := range cfg.CustomRules {
		pattern, err := regexp.Compile(c.Pattern)
		if err != nil {
			return nil, fmt.Errorf("pii: invalid custom rule %q: %w", c.Name, err)
		}
		placeholder := c.Placeholder
		if placeholder == "" {
			placeholder = strings.ToUpper(c.Name)
		}
		rules = append(rules, rule{typ: placeholder, pattern: pattern})
	}

	var recognizer ner.Recognizer
	if !cfg.NER.Disabled {
		recognizer = ner.NewHeuristicRecognizer()
	}

	return &Engine{rules: rules, ner: recognizer}, nil
}

// Detect returns every non-overlapping PII match found in text, combining
// Layer 1 (RegEx) and Layer 2 (NER) results, ordered by position. When
// two candidate matches overlap — whether both from Layer 1, both from
// Layer 2, or one of each — the longer one wins, e.g. a 14-digit SIRET
// candidate wins over a 9-digit SIREN candidate starting at the same
// position.
func (e *Engine) Detect(text string) []Match {
	var candidates []Match
	for _, ru := range e.rules {
		for _, loc := range ru.pattern.FindAllStringIndex(text, -1) {
			raw := text[loc[0]:loc[1]]
			if ru.validate != nil && !ru.validate(raw) {
				continue
			}
			candidates = append(candidates, Match{Type: ru.typ, Value: raw, Start: loc[0], End: loc[1]})
		}
	}
	if e.ner != nil {
		for _, ent := range e.ner.Recognize(text) {
			candidates = append(candidates, Match{Type: string(ent.Type), Value: ent.Value, Start: ent.Start, End: ent.End})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Start != candidates[j].Start {
			return candidates[i].Start < candidates[j].Start
		}
		li := candidates[i].End - candidates[i].Start
		lj := candidates[j].End - candidates[j].Start
		return li > lj
	})

	var selected []Match
	lastEnd := -1
	for _, c := range candidates {
		if c.Start < lastEnd {
			continue
		}
		selected = append(selected, c)
		lastEnd = c.End
	}
	return selected
}
