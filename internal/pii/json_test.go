package pii

import (
	"encoding/json"
	"testing"
)

func TestMaskJSON_MasksNestedStrings(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	table := NewTable()

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"my email is jean.dupont@example.com"}]}`)
	masked, err := MaskJSON(body, e, table)
	if err != nil {
		t.Fatalf("MaskJSON: %v", err)
	}

	var parsed struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(masked, &parsed); err != nil {
		t.Fatalf("masked body is not valid JSON: %v (body=%s)", err, masked)
	}
	if parsed.Model != "gpt-4o" {
		t.Errorf("model = %q, want unchanged %q", parsed.Model, "gpt-4o")
	}
	if parsed.Messages[0].Content != "my email is [EMAIL_1]" {
		t.Errorf("content = %q, want %q", parsed.Messages[0].Content, "my email is [EMAIL_1]")
	}
	if table.Len() != 1 {
		t.Errorf("table.Len() = %d, want 1", table.Len())
	}
}

func TestMaskJSON_NoPII_ReturnsOriginalBytesUnchanged(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	table := NewTable()

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"summarize this document"}]}`)
	masked, err := MaskJSON(body, e, table)
	if err != nil {
		t.Fatalf("MaskJSON: %v", err)
	}
	if string(masked) != string(body) {
		t.Errorf("MaskJSON changed a body with no PII: got %s, want unchanged %s", masked, body)
	}
}

func TestUnmaskJSON_RestoresOriginalValues(t *testing.T) {
	e, err := NewEngine(defaultTestConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	table := NewTable()

	reqBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"my email is jean.dupont@example.com"}]}`)
	if _, err := MaskJSON(reqBody, e, table); err != nil {
		t.Fatalf("MaskJSON: %v", err)
	}

	respBody := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Got it, I'll email [EMAIL_1] the report."}}]}`)
	unmasked, err := UnmaskJSON(respBody, table)
	if err != nil {
		t.Fatalf("UnmaskJSON: %v", err)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(unmasked, &parsed); err != nil {
		t.Fatalf("unmasked body is not valid JSON: %v", err)
	}
	want := "Got it, I'll email jean.dupont@example.com the report."
	if parsed.Choices[0].Message.Content != want {
		t.Errorf("content = %q, want %q", parsed.Choices[0].Message.Content, want)
	}
}

func TestUnmaskJSON_EmptyTable_ReturnsOriginalBytesUnchanged(t *testing.T) {
	table := NewTable()
	body := []byte(`{"id":"chatcmpl-1"}`)
	unmasked, err := UnmaskJSON(body, table)
	if err != nil {
		t.Fatalf("UnmaskJSON: %v", err)
	}
	if string(unmasked) != string(body) {
		t.Errorf("UnmaskJSON changed a body with an empty table: got %s, want unchanged %s", unmasked, body)
	}
}
