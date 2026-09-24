package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func anthropicFrame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func TestAnthropicStreamTranslator_ContentBlockDelta(t *testing.T) {
	p := mustAnthropic(t)
	tr := p.NewStreamTranslator()

	frame := anthropicFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
	out := tr.Feed([]byte(frame))
	out = append(out, tr.Flush()...)

	if !strings.HasPrefix(string(out), "data: ") {
		t.Fatalf("output = %q, want a \"data: \" prefixed OpenAI-shaped frame", out)
	}

	var chunk struct {
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	payload := strings.TrimPrefix(strings.TrimSuffix(string(out), "\n\n"), "data: ")
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("output is not valid JSON: %v (out=%s)", err, out)
	}
	if len(chunk.Choices) != 1 || chunk.Choices[0].Delta.Content != "Hello" {
		t.Errorf("chunk = %+v, want delta.content=%q", chunk, "Hello")
	}
}

func TestAnthropicStreamTranslator_MessageStop_EmitsDone(t *testing.T) {
	p := mustAnthropic(t)
	tr := p.NewStreamTranslator()

	frame := anthropicFrame("message_stop", `{"type":"message_stop"}`)
	out := tr.Feed([]byte(frame))

	want := "data: [DONE]\n\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestAnthropicStreamTranslator_MessageDelta_EmitsFinishReason(t *testing.T) {
	p := mustAnthropic(t)
	tr := p.NewStreamTranslator()

	frame := anthropicFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
	out := tr.Feed([]byte(frame))

	var chunk struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	payload := strings.TrimPrefix(strings.TrimSuffix(string(out), "\n\n"), "data: ")
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("output is not valid JSON: %v (out=%s)", err, out)
	}
	if len(chunk.Choices) != 1 || chunk.Choices[0].FinishReason != "stop" {
		t.Errorf("chunk = %+v, want finish_reason=stop (end_turn mapped)", chunk)
	}
}

// TestAnthropicStreamTranslator_IgnoredEvents covers the native event
// types that have no OpenAI-shaped equivalent and must produce nothing.
func TestAnthropicStreamTranslator_IgnoredEvents(t *testing.T) {
	tests := []struct {
		name  string
		event string
		data  string
	}{
		{"message_start", "message_start", `{"type":"message_start","message":{"id":"msg_1"}}`},
		{"content_block_start", "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_stop", "content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"ping", "ping", `{"type":"ping"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustAnthropic(t)
			tr := p.NewStreamTranslator()
			out := tr.Feed([]byte(anthropicFrame(tt.event, tt.data)))
			if len(out) != 0 {
				t.Errorf("Feed(%s) = %q, want no output", tt.event, out)
			}
		})
	}
}

// TestAnthropicStreamTranslator_SplitAcrossChunks proves the translator
// correctly buffers a native frame split mid-frame across two raw reads
// — the same TCP-fragmentation robustness pii.SSEUnmasker has.
func TestAnthropicStreamTranslator_SplitAcrossChunks(t *testing.T) {
	p := mustAnthropic(t)
	tr := p.NewStreamTranslator()

	frame := anthropicFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`)
	mid := len(frame) / 2

	var out []byte
	out = append(out, tr.Feed([]byte(frame[:mid]))...)
	out = append(out, tr.Feed([]byte(frame[mid:]))...)

	if !strings.Contains(string(out), `"content":"Hi"`) {
		t.Errorf("output = %q, want it to contain the translated delta", out)
	}
}

func TestAnthropicStreamTranslator_FullConversation(t *testing.T) {
	p := mustAnthropic(t)
	tr := p.NewStreamTranslator()

	frames := []string{
		anthropicFrame("message_start", `{"type":"message_start","message":{"id":"msg_1"}}`),
		anthropicFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		anthropicFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello, "}}`),
		anthropicFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world!"}}`),
		anthropicFrame("content_block_stop", `{"type":"content_block_stop","index":0}`),
		anthropicFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`),
		anthropicFrame("message_stop", `{"type":"message_stop"}`),
	}

	var out []byte
	for _, f := range frames {
		out = append(out, tr.Feed([]byte(f))...)
	}
	out = append(out, tr.Flush()...)

	got := string(out)
	if !strings.Contains(got, `"content":"Hello, "`) || !strings.Contains(got, `"content":"world!"`) {
		t.Errorf("missing expected content deltas in output: %s", got)
	}
	if !strings.Contains(got, `"finish_reason":"stop"`) {
		t.Errorf("missing finish_reason in output: %s", got)
	}
	if !strings.HasSuffix(got, "data: [DONE]\n\n") {
		t.Errorf("output does not end with the [DONE] sentinel: %s", got)
	}
}
