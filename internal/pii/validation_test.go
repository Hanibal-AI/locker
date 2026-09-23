package pii

import "testing"

func TestLuhnValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid visa test number", "4111111111111111", true},
		{"valid siren", "123456782", true},
		{"invalid random digits", "123456789", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := luhnValid(tt.input); got != tt.want {
				t.Errorf("luhnValid(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCardLuhn(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid 16-digit card", "4111111111111111", true},
		{"valid 16-digit card, spaced", "4111 1111 1111 1111", true},
		{"9 digits excluded even if luhn-valid (reserved for SIREN)", "123456782", false},
		{"14 digits excluded even if luhn-valid (reserved for SIRET)", "12345678901237", false},
		{"too short", "411111111111", false},
		{"invalid checksum", "4111111111111112", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cardLuhn(tt.input); got != tt.want {
				t.Errorf("cardLuhn(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSirenValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid siren", "123456782", true},
		{"valid siren, grouped by 3", "123 456 782", true},
		{"random 9 digits, not a real siren (false-positive trap)", "123456789", false},
		{"wrong length", "12345678", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sirenValid(tt.input); got != tt.want {
				t.Errorf("sirenValid(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSiretValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid siret", "12345678901237", true},
		{"valid siret, grouped 3-3-3-5", "123 456 789 01237", true},
		{"random 14 digits, not a real siret", "12345678901234", false},
		{"wrong length", "1234567890123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := siretValid(tt.input); got != tt.want {
				t.Errorf("siretValid(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIBANValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid french iban", "FR7630006000011234567890189", true},
		{"valid french iban, spaced", "FR76 3000 6000 0112 3456 7890 189", true},
		{"wrong check digits", "FR7530006000011234567890189", false},
		{"too short", "FR76300060000", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ibanValid(tt.input); got != tt.want {
				t.Errorf("ibanValid(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
