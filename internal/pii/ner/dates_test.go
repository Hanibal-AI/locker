package ner

import "testing"

func TestRecognizeDates(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"iso", "meet on 2024-03-12 please", "2024-03-12"},
		{"numeric slash", "born 12/03/1990", "12/03/1990"},
		{"numeric dot", "born 12.03.1990", "12.03.1990"},
		{"dash dd-mm-yyyy", "signed 12-03-2024", "12-03-2024"},
		{"english month day year", "due March 3, 2024 at noon", "March 3, 2024"},
		{"english month day year no comma", "due March 3rd 2024", "March 3rd 2024"},
		{"french day month year", "signé le 3 mars 2024", "3 mars 2024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := recognizeDates(tt.text)
			if len(entities) != 1 {
				t.Fatalf("recognizeDates(%q) = %d matches, want 1: %+v", tt.text, len(entities), entities)
			}
			if entities[0].Value != tt.want {
				t.Errorf("Value = %q, want %q", entities[0].Value, tt.want)
			}
			if entities[0].Type != TypeDate {
				t.Errorf("Type = %q, want %q", entities[0].Type, TypeDate)
			}
		})
	}
}

func TestRecognizeDates_NoFalsePositiveOnPlainNumbers(t *testing.T) {
	entities := recognizeDates("the invoice total is 1234567 and the ref is 42")
	if len(entities) != 0 {
		t.Errorf("recognizeDates(...) = %+v, want no matches", entities)
	}
}
