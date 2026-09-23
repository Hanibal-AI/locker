package pii

import (
	"bytes"
	"encoding/json"
	"strings"
)

// maxPlaceholderLookback bounds how far back SSEUnmasker looks for the
// start of a not-yet-complete placeholder token. Built-in and custom
// placeholders are short (e.g. "[EMPLOYEE_ID_12]"); this is generous
// headroom.
const maxPlaceholderLookback = 64

// SSEUnmasker restores placeholder tokens in the generated text of an
// OpenAI-compatible Server-Sent Events chat-completion stream.
//
// A naive byte-level pass over the raw stream cannot safely handle a
// placeholder split across two separate delta events (e.g. one SSE
// "data: {...}" event ending in "...[" and the next starting with
// "SIRET_1]...") — the two halves are not adjacent in the raw byte
// stream, since SSE/JSON framing bytes sit between them. SSEUnmasker
// instead parses each complete frame, tracks a pending suffix per choice
// index, and prepends it onto that choice's next delta before unmasking.
// See Docs/roadmap.md Phase 2.4.
//
// Any frame that isn't a recognizable chat-completion-chunk JSON object
// (e.g. the "[DONE]" sentinel, a comment line, a keep-alive) is forwarded
// unchanged.
type SSEUnmasker struct {
	table   *Table
	pending map[int]string // choice index -> not-yet-safe-to-flush suffix
	buf     []byte         // raw bytes not yet forming one complete frame
}

// NewSSEUnmasker builds an SSEUnmasker backed by table.
func NewSSEUnmasker(table *Table) *SSEUnmasker {
	return &SSEUnmasker{table: table, pending: map[int]string{}}
}

// Feed appends chunk to the internal buffer and returns every complete
// SSE frame ("...\n\n"-terminated) it can now emit, with any known
// placeholder fully restored.
func (u *SSEUnmasker) Feed(chunk []byte) []byte {
	u.buf = append(u.buf, chunk...)

	var out bytes.Buffer
	for {
		idx := bytes.Index(u.buf, []byte("\n\n"))
		if idx == -1 {
			break
		}
		frame := u.buf[:idx]
		rest := make([]byte, len(u.buf)-idx-2)
		copy(rest, u.buf[idx+2:])
		u.buf = rest

		out.Write(u.processFrame(frame))
		out.WriteString("\n\n")
	}
	return out.Bytes()
}

// Flush processes whatever partial frame remains at end of stream, plus
// any still-pending per-choice suffixes (which can no longer be completed
// by a following event, so they are emitted raw, best-effort). Call it
// once, at the end of the stream.
func (u *SSEUnmasker) Flush() []byte {
	var out bytes.Buffer
	if len(u.buf) > 0 {
		out.Write(u.processFrame(u.buf))
		u.buf = nil
	}
	for index, suffix := range u.pending {
		out.WriteString(suffix)
		delete(u.pending, index)
	}
	return out.Bytes()
}

func (u *SSEUnmasker) processFrame(frame []byte) []byte {
	const prefix = "data: "
	if !bytes.HasPrefix(frame, []byte(prefix)) {
		return frame
	}
	payload := frame[len(prefix):]

	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		// Not a JSON object (e.g. the "[DONE]" sentinel) — nothing to do.
		return frame
	}

	choices, _ := data["choices"].([]any)
	for _, c := range choices {
		choice, ok := c.(map[string]any)
		if !ok {
			continue
		}
		index := 0
		if f, ok := choice["index"].(float64); ok {
			index = int(f)
		}
		if delta, ok := choice["delta"].(map[string]any); ok {
			u.unmaskField(delta, "content", index)
		}
		// Legacy /v1/completions-style chunks carry text directly on the
		// choice rather than under "delta".
		u.unmaskField(choice, "text", index)
	}

	out, err := json.Marshal(data)
	if err != nil {
		return frame
	}
	return append([]byte(prefix), out...)
}

// unmaskField restores placeholders in obj[field] in place, holding back
// a trailing not-yet-complete placeholder so it can be prepended onto the
// next event for the same choice index.
func (u *SSEUnmasker) unmaskField(obj map[string]any, field string, index int) {
	text, ok := obj[field].(string)
	if !ok {
		return
	}
	if pending, ok := u.pending[index]; ok {
		text = pending + text
		delete(u.pending, index)
	}
	if u.table.Len() == 0 {
		obj[field] = text
		return
	}

	safe, held := splitBeforePendingPlaceholder(text)
	obj[field] = u.table.Unmask(safe)
	if held != "" {
		u.pending[index] = held
	}
}

// splitBeforePendingPlaceholder splits text at the last position that
// cannot be the start of a not-yet-complete placeholder token, so the
// prefix is always safe to unmask and emit now.
func splitBeforePendingPlaceholder(text string) (safe, held string) {
	n := len(text)
	lookback := n
	if lookback > maxPlaceholderLookback {
		lookback = maxPlaceholderLookback
	}
	tailStart := n - lookback

	idx := strings.LastIndexByte(text[tailStart:], '[')
	if idx == -1 {
		return text, ""
	}
	absIdx := tailStart + idx
	if strings.IndexByte(text[absIdx:], ']') == -1 {
		return text[:absIdx], text[absIdx:]
	}
	return text, ""
}
