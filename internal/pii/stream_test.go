package pii

import (
	"strings"
	"testing"
)

func newTableWithEmail(t *testing.T) *Table {
	t.Helper()
	table := NewTable()
	table.forward["[EMAIL_1]"] = "jean.dupont@example.com"
	table.reverse["jean.dupont@example.com"] = "[EMAIL_1]"
	table.counts[TypeEmail] = 1
	return table
}

func newTableWithSIREN(t *testing.T) *Table {
	t.Helper()
	table := NewTable()
	table.forward["[SIREN_1]"] = "123456782"
	table.reverse["123456782"] = "[SIREN_1]"
	table.counts[TypeSIREN] = 1
	return table
}

// sseFrame builds a minimal OpenAI-compatible streaming chunk. Key order
// matches what encoding/json produces when marshaling a map[string]any
// (alphabetical), since SSEUnmasker re-serializes frames that way.
func sseFrame(delta string) string {
	return `data: {"choices":[{"delta":{"content":"` + delta + `"},"index":0}]}` + "\n\n"
}

func TestSSEUnmasker_SingleFrame(t *testing.T) {
	u := NewSSEUnmasker(newTableWithEmail(t), 0)
	out := u.Feed([]byte(sseFrame(`contact [EMAIL_1] now`)))
	out = append(out, u.Flush()...)

	want := sseFrame(`contact jean.dupont@example.com now`)
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestSSEUnmasker_PlaceholderSplitAcrossSeparateEvents reproduces the bug
// found when validating Phase 2 end-to-end with the compiled binary: an
// upstream provider can tokenize a placeholder such that it is split
// across two separate, independently-JSON-encoded SSE events (e.g. one
// event's delta ends in "[" and the very next one starts with
// "SIREN_1]"). A byte-level pass cannot restore this, because SSE/JSON
// framing bytes sit between the two halves in the raw stream; SSEUnmasker
// must track the split per choice index instead.
func TestSSEUnmasker_PlaceholderSplitAcrossSeparateEvents(t *testing.T) {
	u := NewSSEUnmasker(newTableWithSIREN(t), 0)

	frame1 := sseFrame(`our SIREN is [`)
	frame2 := sseFrame(`SIREN_1] on file`)

	var out []byte
	out = append(out, u.Feed([]byte(frame1))...)
	out = append(out, u.Feed([]byte(frame2))...)
	out = append(out, u.Flush()...)

	want := sseFrame(`our SIREN is `) + sseFrame(`123456782 on file`)
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSEUnmasker_PlaceholderSplitWithinOneRawChunk(t *testing.T) {
	// A single Feed() call can also receive a TCP-level fragment that cuts
	// a frame in half; that must still round-trip correctly once the rest
	// of the frame arrives.
	u := NewSSEUnmasker(newTableWithEmail(t), 0)
	full := sseFrame(`reach me at jean.dupont@example.com please`)
	// Replace the real email with its placeholder in the "upstream" frame,
	// as if the model had echoed the masked token back.
	masked := sseFrame(`reach me at [EMAIL_1] please`)

	mid := len(masked) / 2
	var out []byte
	out = append(out, u.Feed([]byte(masked[:mid]))...)
	out = append(out, u.Feed([]byte(masked[mid:]))...)
	out = append(out, u.Flush()...)

	if string(out) != full {
		t.Errorf("output = %q, want %q", out, full)
	}
}

func TestSSEUnmasker_DonePassesThrough(t *testing.T) {
	u := NewSSEUnmasker(newTableWithEmail(t), 0)
	out := u.Feed([]byte("data: [DONE]\n\n"))
	out = append(out, u.Flush()...)

	want := "data: [DONE]\n\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestSSEUnmasker_UnknownPlaceholderLeftAlone(t *testing.T) {
	u := NewSSEUnmasker(NewTable(), 0)
	out := u.Feed([]byte(sseFrame(`looks like [NOT_REAL_1] here`)))
	out = append(out, u.Flush()...)

	want := sseFrame(`looks like [NOT_REAL_1] here`)
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestSSEUnmasker_ConfigurableLookback proves the lookback window
// (Docs/roadmap.md Phase 5.1, config.PIIConfig.StreamLookbackBytes) is a
// real, effective knob: a window too small to cover the placeholder
// fails to reassemble it, while a large-enough window succeeds. The split
// lands a few characters into the token (not right at the "[") so a
// too-small window genuinely has to look past other characters to find
// the opening bracket.
func TestSSEUnmasker_ConfigurableLookback(t *testing.T) {
	// "[SIREN_1]" is 9 bytes long; split after "[SIR".
	frame1 := sseFrame(`our SIREN is [SIR`)
	frame2 := sseFrame(`EN_1] on file`)

	t.Run("lookback too small: placeholder is not reassembled", func(t *testing.T) {
		u := NewSSEUnmasker(newTableWithSIREN(t), 1)
		var out []byte
		out = append(out, u.Feed([]byte(frame1))...)
		out = append(out, u.Feed([]byte(frame2))...)
		out = append(out, u.Flush()...)

		if strings.Contains(string(out), "123456782") {
			t.Errorf("expected the placeholder to survive un-restored with a too-small lookback, got %q", out)
		}
	})

	t.Run("default lookback: placeholder is reassembled", func(t *testing.T) {
		u := NewSSEUnmasker(newTableWithSIREN(t), 0)
		var out []byte
		out = append(out, u.Feed([]byte(frame1))...)
		out = append(out, u.Feed([]byte(frame2))...)
		out = append(out, u.Flush()...)

		if !strings.Contains(string(out), "123456782") {
			t.Errorf("expected the placeholder to be restored with the default lookback, got %q", out)
		}
	})
}

func TestSSEUnmasker_ParallelChoicesTrackedIndependently(t *testing.T) {
	table := NewTable()
	table.forward["[EMAIL_1]"] = "jean.dupont@example.com"
	table.forward["[SIREN_1]"] = "123456782"
	table.counts[TypeEmail] = 1
	table.counts[TypeSIREN] = 1

	u := NewSSEUnmasker(table, 0)
	frame := `data: {"choices":[` +
		`{"index":0,"delta":{"content":"email [EMAIL_1]"}},` +
		`{"index":1,"delta":{"content":"siren [SIREN_1]"}}` +
		`]}` + "\n\n"

	out := u.Feed([]byte(frame))
	out = append(out, u.Flush()...)

	got := string(out)
	if !strings.Contains(got, "email jean.dupont@example.com") {
		t.Errorf("choice 0 not restored: %s", got)
	}
	if !strings.Contains(got, "siren 123456782") {
		t.Errorf("choice 1 not restored: %s", got)
	}
}
