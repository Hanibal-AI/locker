package ner

import "testing"

// typicalPrompt approximates a realistic chat-completion prompt length
// with a mix of entity types, for a representative latency measurement.
const typicalPrompt = `Hi, I work with Martin at Renault in Boulogne and I need help drafting
a message. Please contact Sarah about the invoice due 2024-03-12, and cc
Jean Dupont at jean.dupont@example.com. The meeting with Dr Martin is in
Paris on March 3, 2024, and our vendor is Acme Corp for this deal. Let me
know if you need anything else before the 12/03/2024 deadline.`

// BenchmarkRecognize measures Layer 2 (NER) latency in isolation.
//
// SLA target (Docs/roadmap.md Phase 3.3): sub-10ms per call for a
// typical chat-completion prompt (a few hundred words) — this keeps the
// whole PII pipeline (Layer 1 + Layer 2) well within the low-single-digit
// millisecond overhead budget set for the symbolic layer in Phase 4, and
// nowhere near the cost/latency of an extra LLM call.
func BenchmarkRecognize(b *testing.B) {
	r := NewHeuristicRecognizer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Recognize(typicalPrompt)
	}
}

func BenchmarkRecognize_ShortPrompt(b *testing.B) {
	r := NewHeuristicRecognizer()
	text := "I work with Martin at Renault in Boulogne"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Recognize(text)
	}
}
