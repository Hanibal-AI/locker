// Package ner implements PII Detection Pipeline Layer 2: recognition of
// unstructured entities (person names, organizations, locations, dates)
// that RegEx alone cannot reliably catch. See Docs/roadmap.md Phase 3.
//
// Packaging tradeoff (Phase 3.1): the built-in Recognizer is a
// deterministic, pure-Go, in-process gazetteer + heuristic engine — no
// CGO, no external model file, no sidecar process. This was chosen over
// a statistical model (a Presidio-style Python sidecar, or an
// ONNX-exported transformer called via CGO) for three reasons specific to
// this product:
//
//  1. Determinism/auditability: every tag traces back to an explicit rule
//     (a trigger word, a gazetteer hit, a suffix), consistent with the
//     "Fourmi" symbolic layer this pipeline feeds into (Phase 4) — no
//     black-box confidence scores to explain to a security reviewer.
//  2. Distribution: Locker ships as a single static Go binary / minimal
//     Docker image (Docs/roadmap.md Phase 7). A sidecar process adds a
//     second runtime (Python + model weights) to every deployment target;
//     an ONNX model via CGO complicates cross-compilation to the
//     Linux/macOS/Windows, amd64/arm64 matrix Phase 7 targets.
//  3. Latency/cost: in-process, no IPC hop, no model load time — fits the
//     zero-extra-LLM-call, near-zero-latency positioning of the whole PII
//     pipeline (see Docs/initial.md).
//
// The tradeoff is recall on entities with no textual signal at all (a
// bare name with no trigger word, no gazetteer hit, e.g. an uncommon
// name mentioned with zero context) — the Recognizer interface exists so
// a statistical backend can be swapped in later without touching the
// pipeline (internal/pii/engine.go) that calls it.
//
// Graceful degradation (Docs/roadmap.md Phase 5.3): NewHeuristicRecognizer
// has no external dependency (no model file, no subprocess, no network
// call), so it cannot fail to load — there is no degraded state to fall
// back to today. This is a deliberate consequence of the packaging choice
// above, not an oversight. If a future Recognizer backend is added that
// *can* fail to load (e.g. a sidecar not reachable, a missing model
// file), it must report that failure through pii.NewEngine's existing
// (*Engine, error) return — the same fail-fast-at-startup path already
// used for an invalid custom RegEx rule — rather than silently disabling
// Layer 2 and downgrading detection coverage without telling the operator.
package ner

// EntityType identifies the kind of entity recognized. These values are
// also used directly as the placeholder prefix (e.g. "[PERSON_1]"), so
// they must match Docs/roadmap.md Phase 3.2's tag set: PERSON, ORG, LOC,
// DATE.
type EntityType string

const (
	TypePerson EntityType = "PERSON"
	TypeOrg    EntityType = "ORG"
	TypeLoc    EntityType = "LOC"
	TypeDate   EntityType = "DATE"
)

// Entity is a single recognized occurrence in a piece of text.
type Entity struct {
	Type  EntityType
	Value string
	Start int
	End   int
}

// Recognizer finds named entities in text. See the package doc comment
// for why the built-in implementation (NewHeuristicRecognizer) is
// gazetteer/heuristic-based rather than a statistical model, and how to
// swap in a different backend later.
type Recognizer interface {
	Recognize(text string) []Entity
}
