# Benchmarks

Numbers on this page are reproducible locally:

```bash
go test ./internal/pii/ner/... -bench . -benchmem -run '^$'
```

This page is updated as new benchmarked components land. Full request-path
load/latency benchmarking (Docs/roadmap.md Phase 5.2) and a broader public
benchmarks page (Phase 8.4) come later — this is just the Layer 2 (NER)
number introduced in Phase 3.

## PII Layer 2 — Local NER (`internal/pii/ner`)

**SLA target:** sub-10ms per call for a typical chat-completion prompt
(a few hundred words). See Docs/roadmap.md Phase 3.3 and the
`internal/pii/ner` package doc comment for why this budget matters: it
keeps the whole PII pipeline (Layer 1 RegEx + Layer 2 NER) well within
the near-zero-latency, zero-extra-LLM-call positioning described in
Docs/initial.md.

**Measured** (Intel Core Processor (Haswell, no TSX), `go test -bench`,
single core):

| Benchmark | Input | ns/op | ms/op | Allocs/op |
|---|---|---:|---:|---:|
| `BenchmarkRecognize` | ~90-word mixed-entity prompt | 343,561 | 0.34 | 108 |
| `BenchmarkRecognize_ShortPrompt` | single short sentence | 44,089 | 0.04 | 18 |

Both are roughly **25-200x under the 10ms SLA target**, with margin to
spare once Layer 3 (the symbolic "Fourmi" layer, Phase 4) is added on
top in the same request path.

Exact numbers vary by machine; re-run the command above to reproduce on
your own hardware.
