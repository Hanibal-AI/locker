package providers

import (
	"bytes"
	"encoding/json"
)

// NewStreamTranslator returns a fresh translator for one Anthropic
// streaming response.
func (p *anthropicProvider) NewStreamTranslator() StreamTranslator {
	return &anthropicStreamTranslator{}
}

// anthropicStreamTranslator converts Anthropic's native SSE event stream
// (message_start / content_block_start / content_block_delta /
// content_block_stop / message_delta / message_stop, each as an
// "event: <type>\ndata: {...}\n\n" frame) into OpenAI-compatible
// "data: {...}\n\n" chat-completion-chunk frames:
//
//   - content_block_delta (text_delta) -> a choices[].delta.content chunk
//   - message_delta (carries stop_reason) -> a choices[].finish_reason chunk
//   - message_stop -> the "[DONE]" sentinel OpenAI-compatible clients expect
//   - everything else (message_start, content_block_start/stop, pings) is
//     dropped: OpenAI's stream has no equivalent event for them.
//
// Buffers raw bytes until a complete "\n\n"-terminated frame is
// available, the same framing pii.SSEUnmasker uses downstream.
type anthropicStreamTranslator struct {
	buf []byte
}

func (t *anthropicStreamTranslator) Feed(chunk []byte) []byte {
	t.buf = append(t.buf, chunk...)

	var out bytes.Buffer
	for {
		idx := bytes.Index(t.buf, []byte("\n\n"))
		if idx == -1 {
			break
		}
		frame := t.buf[:idx]
		rest := make([]byte, len(t.buf)-idx-2)
		copy(rest, t.buf[idx+2:])
		t.buf = rest

		for _, translated := range translateAnthropicFrame(frame) {
			out.Write(translated)
		}
	}
	return out.Bytes()
}

// Flush discards any leftover partial frame: a well-formed Anthropic
// stream always ends on a complete message_stop event, so nothing
// meaningful is ever left buffered at end of stream.
func (t *anthropicStreamTranslator) Flush() []byte {
	t.buf = nil
	return nil
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

// translateAnthropicFrame parses one complete native frame (its
// "data: " line) and returns zero or more OpenAI-compatible
// "data: {...}\n\n" frames.
func translateAnthropicFrame(frame []byte) [][]byte {
	var dataLine []byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			dataLine = rest
		}
	}
	if dataLine == nil {
		return nil
	}

	var event anthropicStreamEvent
	if err := json.Unmarshal(dataLine, &event); err != nil {
		return nil
	}

	switch event.Type {
	case "content_block_delta":
		if event.Delta.Type != "text_delta" || event.Delta.Text == "" {
			return nil
		}
		return [][]byte{openAIChunkFrame(map[string]any{
			"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"content": event.Delta.Text}},
			},
		})}

	case "message_delta":
		if event.Delta.StopReason == "" {
			return nil
		}
		return [][]byte{openAIChunkFrame(map[string]any{
			"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": mapAnthropicStopReason(event.Delta.StopReason)},
			},
		})}

	case "message_stop":
		return [][]byte{[]byte("data: [DONE]\n\n")}

	default:
		// message_start, content_block_start, content_block_stop, ping,
		// and anything future/unrecognized: no OpenAI-shaped equivalent.
		return nil
	}
}

func openAIChunkFrame(v map[string]any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	frame := append([]byte("data: "), body...)
	return append(frame, []byte("\n\n")...)
}
