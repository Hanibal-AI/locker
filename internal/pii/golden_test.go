package pii

import "testing"

// TestGolden_MaskThenUnmask pins the exact masked output for a set of
// fixed prompts (combining Layer 1 RegEx and Layer 2 NER matches), and
// checks that unmasking round-trips back to the original — see
// Docs/roadmap.md Phase 2.5 and Phase 3.5.
func TestGolden_MaskThenUnmask(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	tests := []struct {
		name       string
		prompt     string
		wantMasked string
	}{
		{
			name:       "name, phone, and email",
			prompt:     "Please call Jean at 06 12 34 56 78 or email jean.dupont@example.com",
			wantMasked: "Please call [PERSON_1] at [PHONE_1] or email [EMAIL_1]",
		},
		{
			name:       "iban and siret in an hr-style request",
			prompt:     "Transfer the invoice to FR7630006000011234567890189, our SIRET is 12345678901237",
			wantMasked: "Transfer the invoice to [IBAN_1], our SIRET is [SIRET_1]",
		},
		{
			name:       "repeated value reuses the same placeholder",
			prompt:     "jean.dupont@example.com sent it, cc jean.dupont@example.com too",
			wantMasked: "[EMAIL_1] sent it, cc [EMAIL_1] too",
		},
		{
			name:       "no pii, unchanged",
			prompt:     "Summarize the attached quarterly report in three bullet points",
			wantMasked: "Summarize the attached quarterly report in three bullet points",
		},
		{
			// The exact Phase 3.5 deliverable example.
			name:       "person, org, and loc (Phase 3 deliverable example)",
			prompt:     "I work with Martin at Renault in Boulogne",
			wantMasked: "I work with [PERSON_1] at [ORG_1] in [LOC_1]",
		},
		{
			name:       "explicit date",
			prompt:     "The contract was signed on 2024-03-12",
			wantMasked: "The contract was signed on [DATE_1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := NewTable()
			masked := table.Mask(tt.prompt, e.Detect(tt.prompt))
			if masked != tt.wantMasked {
				t.Fatalf("masked = %q, want %q", masked, tt.wantMasked)
			}
			if restored := table.Unmask(masked); restored != tt.prompt {
				t.Errorf("Unmask(Mask(x)) = %q, want original %q", restored, tt.prompt)
			}
		})
	}
}
