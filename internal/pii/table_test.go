package pii

import (
	"testing"

	"github.com/Hanibal-AI/locker/internal/config"
)

func TestTable_MaskUnmask_Roundtrip(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	table := NewTable()

	original := "reach jean.dupont@example.com or SIREN 123456782"
	masked := table.Mask(original, e.Detect(original))

	if masked == original {
		t.Fatalf("Mask did not change the text: %q", masked)
	}
	if got := table.Unmask(masked); got != original {
		t.Errorf("Unmask(Mask(x)) = %q, want original %q", got, original)
	}
}

func TestTable_ReusesPlaceholderForRepeatedValue(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	table := NewTable()

	text := "email jean.dupont@example.com or jean.dupont@example.com again"
	masked := table.Mask(text, e.Detect(text))

	if got := table.Len(); got != 1 {
		t.Errorf("Table.Len() = %d, want 1 (same value masked twice should reuse one placeholder)", got)
	}
	want := "email [EMAIL_1] or [EMAIL_1] again"
	if masked != want {
		t.Errorf("masked = %q, want %q", masked, want)
	}
}

func TestTable_UnmaskLeavesUnknownPlaceholdersAlone(t *testing.T) {
	table := NewTable()
	text := "this looks like [NOT_A_REAL_1] placeholder"
	if got := table.Unmask(text); got != text {
		t.Errorf("Unmask(%q) = %q, want unchanged", text, got)
	}
}

func defaultTestConfig() config.PIIConfig { return config.PIIConfig{} }
