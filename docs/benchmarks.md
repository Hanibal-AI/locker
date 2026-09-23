# Benchmarks

Numbers on this page are reproducible locally:

```bash
go test ./internal/pii/ner/... -bench . -benchmem -run '^$'
go test ./internal/proxy/... -bench . -benchmem -run '^$'
go run ./scripts/loadtest -concurrency 50 -requests 5000          # non-streaming
go run ./scripts/loadtest -concurrency 50 -requests 5000 -stream  # streaming
```

This page is updated as new benchmarked components land. A broader public
benchmarks page (Phase 8.4) comes later.

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

## Full pipeline — request handling (`internal/proxy`)

**Measured** (`go test -bench`, full pipeline: RegEx + NER masking on the
request, forwarding, response unmasking):

| Benchmark | ns/op | ms/op | Allocs/op |
|---|---:|---:|---:|
| `BenchmarkNonStreaming` | 574,364 | 0.57 | 295 |
| `BenchmarkNonStreaming_Parallel` | 387,476 | 0.39 | 330 |
| `BenchmarkStreaming` | 645,088 | 0.65 | 308 |
| `BenchmarkStreaming_Parallel` | 454,393 | 0.45 | 342 |

For scale, a real LLM completion typically takes anywhere from ~100ms
(short, non-streaming) to several seconds (long, streamed) — Locker's own
processing overhead here is a small fraction of a single upstream
round-trip, not something a user would notice.

## Load test — real process, concurrent sessions (`scripts/loadtest`)

Unlike the benchmarks above (in-memory `ServeHTTP` calls, same process),
this drives a real Locker HTTP server over real sockets with a fake
OpenAI-compatible upstream, exercising actual concurrent connection
handling — see Docs/roadmap.md Phase 5.2.

**Measured**, 50 concurrent clients, 5,000 requests:

| Mode | req/s | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|
| Non-streaming | 4,705 | 8.3ms | 29.5ms | 45.3ms | 90.6ms |
| Streaming | 4,375 | 10.4ms | 27.9ms | 46.8ms | 81.8ms |

**Measured**, 200 concurrent clients, 20,000 requests (non-streaming):

| Mode | req/s | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|
| Non-streaming | 4,870 | 32.9ms | 110.1ms | 158.3ms | 348.2ms |

No failed requests at either concurrency level. This load test is also
what caught a real bug during Phase 5 hardening: a Content-Length header
copied from the upstream (matching the *masked* response length) was
being committed before the response was unmasked, and unmasking almost
always changes the body length — this broke real HTTP connections under
load even though it was invisible to `httptest.NewRecorder()`-based unit
tests, which don't enforce wire-protocol framing. Fixed in
`internal/proxy/proxy.go` by deferring `Content-Length`/`WriteHeader`
until the final (unmasked) body is known; see
`internal/proxy/resilience_test.go` for the regression test.
