package ner

import "testing"

func recognize(text string) []Entity {
	return NewHeuristicRecognizer().Recognize(text)
}

func findType(entities []Entity, typ EntityType) (Entity, bool) {
	for _, e := range entities {
		if e.Type == typ {
			return e, true
		}
	}
	return Entity{}, false
}

// TestDeliverableExample is the exact Phase 3.5 deliverable example.
func TestDeliverableExample(t *testing.T) {
	entities := recognize("I work with Martin at Renault in Boulogne")

	person, ok := findType(entities, TypePerson)
	if !ok || person.Value != "Martin" {
		t.Errorf("PERSON = %+v, ok=%v, want Value=Martin", person, ok)
	}
	org, ok := findType(entities, TypeOrg)
	if !ok || org.Value != "Renault" {
		t.Errorf("ORG = %+v, ok=%v, want Value=Renault", org, ok)
	}
	loc, ok := findType(entities, TypeLoc)
	if !ok || loc.Value != "Boulogne" {
		t.Errorf("LOC = %+v, ok=%v, want Value=Boulogne", loc, ok)
	}
	if len(entities) != 3 {
		t.Errorf("got %d entities, want exactly 3: %+v", len(entities), entities)
	}
}

func TestRecognize_TruePositives(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantType EntityType
		wantVal  string
	}{
		{"person via trigger", "please contact Sarah about the invoice", TypePerson, "Sarah"},
		{"person via title", "ask Dr Martin for a signature", TypePerson, "Martin"},
		{"person via two-word gazetteer first name", "Jean Dupont sent the file", TypePerson, "Jean Dupont"},
		{"org via legal suffix", "our vendor is Acme Corp for this deal", TypeOrg, "Acme Corp"},
		{"org via trigger chez", "il travaille chez Google désormais", TypeOrg, "Google"},
		{"loc via gazetteer", "the meeting is in Paris next week", TypeLoc, "Paris"},
		{"loc via french trigger", "elle habite à Lyon", TypeLoc, "Lyon"},
		{"date iso", "deadline is 2024-03-12 sharp", TypeDate, "2024-03-12"},
		{"date numeric eu", "born on 12/03/1990", TypeDate, "12/03/1990"},
		{"date month name english", "due March 3, 2024 at noon", TypeDate, "March 3, 2024"},
		{"date day month year french", "signé le 3 mars 2024", TypeDate, "3 mars 2024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := recognize(tt.text)
			got, ok := findType(entities, tt.wantType)
			if !ok {
				t.Fatalf("Recognize(%q) has no %s entity: %+v", tt.text, tt.wantType, entities)
			}
			if got.Value != tt.wantVal {
				t.Errorf("Value = %q, want %q", got.Value, tt.wantVal)
			}
		})
	}
}

// TestRecognize_AmbiguousNames covers names that are also common words —
// the false-positive/negative trade-off called for by Docs/roadmap.md
// Phase 3.4. Without context, these must NOT be flagged (high precision);
// with a trigger word or gazetteer hit, they must be (acceptable recall).
func TestRecognize_AmbiguousNames(t *testing.T) {
	t.Run("bare ambiguous word not in the gazetteer, no context, is not flagged", func(t *testing.T) {
		// "Will" and "May" are common given names but deliberately absent
		// from firstNames (see gazetteers.go): with zero context, that
		// keeps precision high on their much more common modal-verb use.
		for _, text := range []string{
			"Will this work for the demo",
			"May I ask a quick question",
		} {
			entities := recognize(text)
			if _, ok := findType(entities, TypePerson); ok {
				t.Errorf("Recognize(%q) flagged a PERSON with no supporting context: %+v", text, entities)
			}
		}
	})

	t.Run("same words, with context, are flagged", func(t *testing.T) {
		tests := []struct {
			text    string
			wantVal string
		}{
			{"please contact Will about the demo", "Will"},
			{"please contact May about the invoice", "May"},
		}
		for _, tt := range tests {
			entities := recognize(tt.text)
			got, ok := findType(entities, TypePerson)
			if !ok || got.Value != tt.wantVal {
				t.Errorf("Recognize(%q) PERSON = %+v, ok=%v, want Value=%q", tt.text, got, ok, tt.wantVal)
			}
		}
	})

	t.Run("known tradeoff: a gazetteer name is flagged even with zero context", func(t *testing.T) {
		// Unlike "Will"/"May" above, "Grace" IS in firstNames (it's a
		// common given name), so rule 7 fires on the bare word alone —
		// this is the recall/precision tradeoff documented in the
		// package doc comment: good recall on common names used as a
		// bare subject, at the cost of an occasional false positive when
		// the same word is used with its ordinary meaning (e.g. "grace
		// under pressure"). A statistical model would resolve this with
		// grammatical context; this heuristic engine does not attempt to.
		entities := recognize("Grace called earlier about the order")
		got, ok := findType(entities, TypePerson)
		if !ok || got.Value != "Grace" {
			t.Errorf("Recognize(...) PERSON = %+v, ok=%v, want Value=Grace", got, ok)
		}
	})
}

func TestRecognize_NoFalsePositiveOnSentenceInitialCapital(t *testing.T) {
	// A capitalized word only because it starts the sentence, with no
	// other signal, must not be flagged as any entity type.
	entities := recognize("Summarize the attached quarterly report please")
	if len(entities) != 0 {
		t.Errorf("Recognize(...) = %+v, want no entities", entities)
	}
}

func TestRecognize_OrgVsLocDisambiguation(t *testing.T) {
	// Both use the "at" trigger in English; the gazetteer hit for a known
	// city must win over the default org-trigger interpretation.
	orgEntities := recognize("she works at Renault")
	if org, ok := findType(orgEntities, TypeOrg); !ok || org.Value != "Renault" {
		t.Errorf("expected ORG=Renault, got %+v", orgEntities)
	}

	locEntities := recognize("she works at Boulogne")
	if loc, ok := findType(locEntities, TypeLoc); !ok || loc.Value != "Boulogne" {
		t.Errorf("expected LOC=Boulogne (gazetteer wins over default org trigger), got %+v", locEntities)
	}
}

func TestRecognize_Positions(t *testing.T) {
	text := "contact Sarah today"
	entities := recognize(text)
	person, ok := findType(entities, TypePerson)
	if !ok {
		t.Fatalf("no PERSON found in %+v", entities)
	}
	if text[person.Start:person.End] != "Sarah" {
		t.Errorf("text[%d:%d] = %q, want %q", person.Start, person.End, text[person.Start:person.End], "Sarah")
	}
}
