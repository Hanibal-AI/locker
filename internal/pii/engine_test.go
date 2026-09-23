package pii

import (
	"testing"

	"github.com/Hanibal-AI/locker/internal/config"
)

func mustEngine(t *testing.T, cfg config.PIIConfig) *Engine {
	t.Helper()
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}
	return e
}

func TestEngine_Detect_TruePositives(t *testing.T) {
	e := mustEngine(t, config.PIIConfig{})

	tests := []struct {
		name     string
		text     string
		wantType string
		wantVal  string
	}{
		{"email", "contact me at jean.dupont@example.com please", TypeEmail, "jean.dupont@example.com"},
		{"french phone", "call me at 06 12 34 56 78 tomorrow", TypePhone, "06 12 34 56 78"},
		{"iban", "wire it to FR7630006000011234567890189 today", TypeIBAN, "FR7630006000011234567890189"},
		{"credit card", "card number 4111 1111 1111 1111 expires soon", TypeCreditCard, "4111 1111 1111 1111"},
		{"siren", "our SIREN is 123456782 for this company", TypeSIREN, "123456782"},
		{"siret", "our SIRET is 12345678901237 for this site", TypeSIRET, "12345678901237"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := e.Detect(tt.text)
			if len(matches) != 1 {
				t.Fatalf("Detect(%q) = %d matches, want 1: %+v", tt.text, len(matches), matches)
			}
			if matches[0].Type != tt.wantType {
				t.Errorf("Type = %q, want %q", matches[0].Type, tt.wantType)
			}
			if matches[0].Value != tt.wantVal {
				t.Errorf("Value = %q, want %q", matches[0].Value, tt.wantVal)
			}
		})
	}
}

func TestEngine_Detect_FalsePositiveTraps(t *testing.T) {
	e := mustEngine(t, config.PIIConfig{})

	tests := []struct {
		name string
		text string
	}{
		{"internal product code that happens to be 9 digits", "product code PRD-123456789 is discontinued"},
		{"internal product code that happens to be 14 digits", "batch reference 12345678901234 was recalled"},
		{"plain sentence with no PII", "please summarize the quarterly report for the board"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := e.Detect(tt.text)
			if len(matches) != 0 {
				t.Errorf("Detect(%q) = %+v, want no matches (failed checksum should reject it)", tt.text, matches)
			}
		})
	}
}

func TestEngine_Detect_SIRETPreferredOverSIREN(t *testing.T) {
	// Grouped with spaces, the first 9 digits of a SIRET are themselves a
	// valid SIREN-shaped, boundary-to-boundary candidate ("123 456 789").
	// Detect must resolve the overlap by keeping the longer SIRET match
	// rather than reporting both.
	e := mustEngine(t, config.PIIConfig{})
	text := "siret 123 456 789 01237 on file"

	matches := e.Detect(text)
	if len(matches) != 1 {
		t.Fatalf("Detect(%q) = %d matches, want 1 (SIRET should win, not overlap with SIREN): %+v", text, len(matches), matches)
	}
	if matches[0].Type != TypeSIRET {
		t.Errorf("Type = %q, want %q", matches[0].Type, TypeSIRET)
	}
}

func TestEngine_Disabled(t *testing.T) {
	e := mustEngine(t, config.PIIConfig{Disabled: true})
	matches := e.Detect("email me at jean.dupont@example.com")
	if len(matches) != 0 {
		t.Errorf("Detect on a disabled engine = %+v, want no matches", matches)
	}
}

func TestEngine_EnabledRules_Subset(t *testing.T) {
	e := mustEngine(t, config.PIIConfig{EnabledRules: []string{"email"}})
	text := "jean.dupont@example.com and SIREN 123456782"

	matches := e.Detect(text)
	if len(matches) != 1 || matches[0].Type != TypeEmail {
		t.Fatalf("Detect(%q) = %+v, want only the email match (siren rule disabled)", text, matches)
	}
}

func TestEngine_CustomRule(t *testing.T) {
	e := mustEngine(t, config.PIIConfig{
		CustomRules: []config.PIIRule{
			{Name: "employee_id", Pattern: `EMP-\d{6}`, Placeholder: "EMPLOYEE_ID"},
		},
	})
	text := "assigned to EMP-482913 for review"

	matches := e.Detect(text)
	if len(matches) != 1 {
		t.Fatalf("Detect(%q) = %+v, want 1 match", text, matches)
	}
	if matches[0].Type != "EMPLOYEE_ID" || matches[0].Value != "EMP-482913" {
		t.Errorf("match = %+v, want Type=EMPLOYEE_ID Value=EMP-482913", matches[0])
	}
}

func TestNewEngine_InvalidCustomRule(t *testing.T) {
	_, err := NewEngine(config.PIIConfig{
		CustomRules: []config.PIIRule{{Name: "bad", Pattern: "("}},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid custom regex, got nil")
	}
}
