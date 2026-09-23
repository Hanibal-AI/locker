package ner

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var tokenPattern = regexp.MustCompile(`[\p{L}][\p{L}'-]*`)

type token struct {
	text        string
	start, end  int
	capitalized bool
}

type heuristicRecognizer struct{}

// NewHeuristicRecognizer builds the built-in, deterministic gazetteer +
// heuristic Recognizer. See the package doc comment (ner.go) for the
// reasoning behind this choice over a statistical model.
func NewHeuristicRecognizer() Recognizer {
	return &heuristicRecognizer{}
}

func (r *heuristicRecognizer) Recognize(text string) []Entity {
	var entities []Entity
	entities = append(entities, recognizeDates(text)...)
	entities = append(entities, recognizeProperNouns(text)...)
	return entities
}

func tokenize(text string) []token {
	locs := tokenPattern.FindAllStringIndex(text, -1)
	tokens := make([]token, 0, len(locs))
	for _, loc := range locs {
		s := text[loc[0]:loc[1]]
		first, _ := utf8.DecodeRuneInString(s)
		tokens = append(tokens, token{
			text:        s,
			start:       loc[0],
			end:         loc[1],
			capitalized: unicode.IsUpper(first),
		})
	}
	return tokens
}

// recognizeProperNouns finds maximal runs of consecutive capitalized
// tokens (e.g. "Jean Dupont") and classifies each run as PERSON, ORG, or
// LOC using an explicit rule cascade (see classifyRun). A run with no
// matching signal is not reported at all, to keep precision high on
// ambiguous bare capitalized words — a name that is also a common word
// (e.g. "Will", "May", "Rose") is only tagged when context backs it up.
func recognizeProperNouns(text string) []Entity {
	tokens := tokenize(text)
	var entities []Entity

	i := 0
	for i < len(tokens) {
		if !tokens[i].capitalized {
			i++
			continue
		}
		j := i + 1
		for j < len(tokens) && tokens[j].capitalized && onlyWhitespaceBetween(text, tokens[j-1].end, tokens[j].start) {
			j++
		}

		// A leading title directly prefixing the rest of the run (e.g.
		// "Dr Martin", no period) is a strong, unambiguous PERSON signal.
		// Titles are themselves capitalized, so without this check they
		// would just be absorbed into the run as its first token instead
		// of being recognized as a trigger for it.
		if j-i > 1 {
			if _, ok := titles[strings.ToLower(tokens[i].text)]; ok {
				name := tokens[i+1 : j]
				entities = append(entities, Entity{
					Type:  TypePerson,
					Value: text[name[0].start:name[len(name)-1].end],
					Start: name[0].start,
					End:   name[len(name)-1].end,
				})
				i = j
				continue
			}
		}

		run := tokens[i:j]

		var before *token
		if i > 0 {
			before = &tokens[i-1]
		}
		var after *token
		if j < len(tokens) {
			after = &tokens[j]
		}

		if ent, ok := classifyRun(text, run, before, after); ok {
			entities = append(entities, ent)
		}
		i = j
	}
	return entities
}

func onlyWhitespaceBetween(text string, start, end int) bool {
	return strings.TrimSpace(text[start:end]) == ""
}

// classifyRun applies the rule cascade described in the package doc
// comment: person trigger > title > exact place gazetteer hit > location
// trigger > org suffix > org trigger > first-name gazetteer hit. Earlier
// rules win on conflict (e.g. "at Boulogne" is LOC via the gazetteer hit,
// even though "at" is also an org trigger, because the gazetteer check
// runs first).
func classifyRun(text string, run []token, before, after *token) (Entity, bool) {
	span := func(extra *token) Entity {
		end := run[len(run)-1].end
		if extra != nil {
			end = extra.end
		}
		return Entity{Value: text[run[0].start:end], Start: run[0].start, End: end}
	}

	beforeLower := ""
	if before != nil {
		beforeLower = strings.ToLower(before.text)
	}

	// An org legal suffix is a strong, specific signal — checked first so
	// it wins even when a generic trigger word like "contact" precedes
	// the run. Suffixes are themselves capitalized, so for an adjacent
	// name+suffix ("Acme Corp") the suffix ends up as the run's own last
	// token rather than a separate "after" token; check that case here,
	// and fall back to the separate-token case (e.g. "Acme, Inc" with a
	// comma breaking the merge) via the `after` check further down.
	if len(run) > 1 {
		last := run[len(run)-1]
		if _, ok := orgSuffixes[strings.ToLower(last.text)]; ok {
			e := span(nil)
			e.Type = TypeOrg
			return e, true
		}
	}

	if _, ok := personTriggers[beforeLower]; ok {
		e := span(nil)
		e.Type = TypePerson
		return e, true
	}
	if _, ok := titles[beforeLower]; ok {
		e := span(nil)
		e.Type = TypePerson
		return e, true
	}

	runLower := strings.ToLower(text[run[0].start:run[len(run)-1].end])
	if _, ok := placeNames[runLower]; ok {
		e := span(nil)
		e.Type = TypeLoc
		return e, true
	}
	if _, ok := locTriggers[beforeLower]; ok {
		e := span(nil)
		e.Type = TypeLoc
		return e, true
	}

	if after != nil {
		if _, ok := orgSuffixes[strings.ToLower(after.text)]; ok {
			e := span(after)
			e.Type = TypeOrg
			return e, true
		}
	}
	if _, ok := orgTriggers[beforeLower]; ok {
		e := span(nil)
		e.Type = TypeOrg
		return e, true
	}

	if _, ok := firstNames[strings.ToLower(run[0].text)]; ok {
		e := span(nil)
		e.Type = TypePerson
		return e, true
	}

	return Entity{}, false
}
