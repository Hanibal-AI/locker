# Locker — Open Source Core Roadmap

## Scope

This roadmap covers the build-out of the **open source core only** (the Locker proxy, as defined in `initial.md`), from empty repository to a v1.0 public release. It does not include the commercial Control Plane / SaaS layer — that is a separate roadmap, to be written once the core is stable and adopted.

The plan is organized as sequential phases. Each phase has concrete steps and sub-steps as checkboxes, and ends with a checkable deliverable. Check items off as work is completed so this file always reflects where the project actually stands.

---

## Phase 0 — Repository & Project Foundations

**Goal:** a clean, buildable, empty-but-structured Go repository that anyone can clone and build on day one.

- [x] **0.1 Repository setup**
  - Create the single monorepo (`locker`) on GitHub under a permissive license (Apache 2.0 or MIT — decide and add `LICENSE`).
  - Add `README.md` (already done), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md` (how to report vulnerabilities).
  - Set up `.gitignore`, `go.mod` / `go.sum`, Go version pin.
- [x] **0.2 Repository layout**
  - `/cmd/locker` — CLI entrypoint (`main.go`).
  - `/internal/proxy` — HTTP/HTTPS reverse proxy core.
  - `/internal/providers` — LLM provider adapters (OpenAI, Anthropic, Mistral).
  - `/internal/pii` — detection pipeline (regex, NER, symbolic layer).
  - `/internal/config` — YAML / env var config loader.
  - `/charts/locker` — Helm chart.
  - `/deploy` — Docker Compose examples, Kubernetes manifests.
  - `/docs` — public documentation (user-facing, distinct from internal `Docs/`).
- [x] **0.3 Tooling & CI baseline**
  - `Makefile` or `Taskfile` with `build`, `test`, `lint`, `run` targets.
  - GitHub Actions: lint (`golangci-lint`), unit tests, build-on-PR.
  - Pre-commit hooks (formatting, `go vet`).
- [x] **0.4 Deliverable** — empty proxy binary that compiles, runs, and returns `200 OK` on a health-check endpoint (`/healthz`).

---

## Phase 1 — Minimal Viable Proxy (Pass-Through, No PII Logic Yet)

**Goal:** prove the drop-in-replacement concept end-to-end before adding any intelligence.

- [x] **1.1 HTTP reverse proxy engine**
  - Implement request forwarding to a configurable upstream (`/v1/chat/completions` → provider URL).
  - Support standard headers, timeouts, retries, and error propagation.
- [x] **1.2 Provider adapters (v1: OpenAI only)**
  - Normalize outbound request/response to the OpenAI Chat Completions schema.
  - API key injection from config/env (never logged, never persisted in plaintext).
- [x] **1.3 Configuration loader**
  - `config.yaml` schema v1: provider API keys, allowed models, listen address/port.
  - Environment variable overrides (12-factor style).
- [x] **1.4 Streaming support (SSE) — pass-through only**
  - Forward `stream: true` responses token-by-token without buffering (no masking yet).
- [x] **1.5 Deliverable** — `docker run locker` successfully proxies a real chat completion request/response, streaming included, with zero transformation — validated against the official OpenAI Python/JS SDKs pointed at `BASE_URL=http://localhost:8080/v1`.
  - Validated locally with the compiled binary end-to-end against a stand-in OpenAI-compatible server: non-streaming pass-through (status + body + upstream headers forwarded unchanged), streaming SSE forwarded chunk-by-chunk, and `/healthz`. Full Docker packaging is Phase 7 — this phase proves the proxy logic itself.

---

## Phase 2 — PII Detection Pipeline, Layer 1: RegEx & Validation Formulas

**Goal:** first real anonymization layer — deterministic, structural, no NLP yet.

- [x] **2.1 RegEx rule engine**
  - Built-in patterns: email, phone number, IBAN, generic credit card shape.
  - Rule format defined in `config.yaml` so custom RegEx rules can be added without code changes.
- [x] **2.2 Validation formulas (reduce false positives)**
  - Luhn algorithm for credit card numbers.
  - SIREN/SIRET checksum validation (French business IDs).
  - IBAN checksum (mod-97) validation.
- [x] **2.3 Masking & re-identification table**
  - In-memory, request-scoped table: original value ↔ placeholder token (`[EMAIL_1]`, `[CB_1]`).
  - Restore original values in the response before returning to the caller.
- [x] **2.4 Non-streaming path first, then streaming**
  - Implement masking on full (non-streamed) requests/responses first.
  - Extend to streaming: buffer just enough tokens to safely detect+mask patterns spanning multiple SSE chunks, then flush — measure and cap added latency.
  - Streaming turned out to need SSE/JSON-frame awareness, not just raw-byte buffering: a placeholder can be split across two separate, independently-encoded `data: {...}` events (e.g. a token boundary lands inside `[SIREN_1]`), which a byte-level pass cannot reassemble since JSON/SSE framing bytes sit between the two halves. `pii.SSEUnmasker` parses each frame and tracks a pending suffix per `choices[].index` instead.
- [x] **2.5 Testing**
  - Unit tests per pattern (true positives, known false-positive traps: SIREN vs. credit card, internal product codes, etc.).
  - Golden-file tests: fixed input prompt → expected masked output.
- [x] **2.6 Deliverable** — a request containing an email, phone number, IBAN, or card number is provably masked before hitting the LLM provider, and correctly restored in the response — demoable end-to-end, streaming included.
  - Validated with `internal/pii` unit tests (91% coverage: validation formulas, engine detection/overlap resolution, table mask/unmask, JSON walking, SSE unmasking) and `internal/proxy` end-to-end tests proving the upstream never sees raw PII and the client always gets it restored, streaming included. Also re-validated against the compiled binary with a real OpenAI-shaped fake upstream, including the SSE placeholder-split scenario above (found and fixed during this validation, not left as a known issue).

---

## Phase 3 — PII Detection Pipeline, Layer 2: Local NER

**Goal:** catch unstructured PII (names, organizations, locations) that RegEx cannot.

- [x] **3.1 Model selection & integration**
  - Evaluate lightweight local NER options (Presidio-style pipelines, spaCy models via a local process, or ONNX-exported models callable from Go/Cgo).
  - Decide on packaging: embedded binary vs. local sidecar process — document the tradeoff (startup time, image size, footprint).
  - Decision: a deterministic, pure-Go, in-process gazetteer + heuristic `Recognizer` (`internal/pii/ner`) — no CGO, no sidecar, no model file. Chosen over a statistical model given this sandbox's constraints (no ONNX runtime, no Python/spaCy available) and, more importantly, the product's own priorities: determinism/auditability consistent with the Phase 4 symbolic layer, and a single static Go binary across the Phase 7 distribution matrix. Tradeoff: lower recall on a bare name with zero surrounding context — documented in the package doc comment. The `Recognizer` interface exists precisely so a statistical backend can be swapped in later without touching the pipeline.
- [x] **3.2 NER pipeline wiring**
  - Run NER on the prompt text after RegEx pass; merge entity spans without double-masking overlaps already caught by RegEx.
  - Tag entities: `PERSON`, `ORG`, `LOC`, `DATE`.
  - `pii.Engine.Detect` merges Layer 1 (RegEx) and Layer 2 (NER) candidates into one pool before the existing overlap-resolution pass, so a RegEx match and an NER match are never both kept for the same span.
- [x] **3.3 Performance budget**
  - Benchmark NER pass latency; set an explicit SLA target (e.g., sub-10ms for typical prompt length) and track it in CI benchmarks.
  - SLA target set at sub-10ms; measured at **0.34ms** for a ~90-word mixed-entity prompt and **0.04ms** for a short one (`go test ./internal/pii/ner/... -bench .`) — 25-200x under budget. Published in `docs/benchmarks.md`; full CI regression tracking is Phase 5/8, not this phase.
- [x] **3.4 Testing**
  - Ambiguous-name test suite (e.g., names that are also common words) to measure false-positive/negative rates.
  - Covers: words absent from the gazetteer (e.g. "Will", "May") correctly unflagged with zero context and flagged with a trigger word; and the converse documented tradeoff — a gazetteer name ("Grace") is flagged even with zero context, since the heuristic has no grammatical understanding to resolve that ambiguity the way a statistical model could.
- [x] **3.5 Deliverable** — a prompt like *"I work with Martin at Renault in Boulogne"* is correctly tagged and masked (`[PERSON_1]`, `[ORG_1]`, `[LOC_1]`) with measured latency published in the repo's benchmarks doc.
  - Pinned as a golden test (`TestGolden_MaskThenUnmask`) and re-verified against the compiled binary end-to-end: the upstream fake server logged receiving exactly `"I work with [PERSON_1] at [ORG_1] in [LOC_1]"`, and the client response came back with the original names restored.

---

## Phase 4 (Later..)— Symbolic Reasoning Layer ("Fourmi" Ontology)

**Goal:** move from "mask tokens" to "understand structure and qualify risk," fully deterministic, in-process.

- [ ] **4.1 Ontology & data model**
  - Define the core node/edge types in Go structs: `Action`, `Actor`, `Resource`, `Risk`, `Link`, etc. (subset of the full v0.2 spec relevant to the open source core).
  - Define the JSON v0.2 payload schema and a versioned Go marshaler for it.
- [ ] **4.2 Entity-to-ontology mapping**
  - Map RegEx/NER output onto ontology nodes (e.g., a `PERSON` entity → `Actor` node candidate).
  - Map the sentence's verb/action onto an `Action` node using a lightweight parser (dependency parsing or rule-based verb extraction — decide based on Phase 3 NER library capabilities).
- [ ] **4.3 Rule matrices (deterministic risk qualification)**
  - Matrix 1: Action × data sensitivity (e.g., verb "export"/"share" + high-sensitivity resource → `Risk` node).
  - Matrix 2: Actor × permitted scope, where scope is defined locally in config (no enterprise SSO/RBAC integration in the open source core — scope is a static config mapping, not a directory sync).
  - Matrix 3: pattern-based prompt-injection / jailbreak phrase detection.
  - Each rule carries a stable ID (e.g., `RULE-104`) for auditability.
- [ ] **4.4 Context-aware masking**
  - Use the ontology mapping to decide *how* to mask an entity (preserve grammatical role) instead of blind token replacement — validate this measurably improves LLM response quality versus Phase 2/3 blind masking (A/B prompt quality comparison).
- [ ] **4.5 Output**
  - Emit the JSON v0.2 graph payload as an optional response header / side-channel (e.g., `X-Locker-Qualification` header or a `/v1/chat/completions?debug=true` mode) — consumption/visualization is explicitly out of scope for the open source core (that's the commercial Control Plane's job).
- [ ] **4.6 Testing**
  - Rule-by-rule unit tests with fixed inputs → expected `Risk` node + edges.
  - End-to-end example matching the one from `initial.md` (HR salary file example) as a regression test.
- [ ] **4.7 Deliverable** — the proxy produces a deterministic, versioned JSON graph for every request, with documented rule IDs, in under ~5ms added latency — published as a reference example in the repo docs.

---

## Phase 5 — Streaming, Performance & Hardening

**Goal:** make the full pipeline (RegEx + NER + symbolic layer (later)) production-grade under real streaming and load conditions. Phase 4 (the symbolic "Fourmi" layer) was deliberately skipped ahead of this phase; this phase hardens what exists today (Layer 1 + Layer 2) and the symbolic layer, when built, inherits the same hardening.

- [x] **5.1 Streaming correctness under the full pipeline**
  - Re-validate SSE buffering strategy now that NER + symbolic layer are in the loop; ensure buffering window is as small as possible while still catching multi-token PII.
  - Add configurable buffer/flush strategy (latency vs. accuracy tradeoff exposed in config).
  - `pii.SSEUnmasker`'s lookback window is now a constructor parameter (`config.PIIConfig.StreamLookbackBytes` / `LOCKER_PII_STREAM_LOOKBACK_BYTES`), proven with a test showing a too-small window genuinely fails to reassemble a split placeholder while the default succeeds. Re-validated that an NER-tagged placeholder (`[PERSON_1]`, not just a RegEx one) split across two separate SSE events is restored correctly with no pipeline change needed — NER matches flow through the same `Table`/placeholder mechanism as RegEx matches.
- [x] **5.2 Load & latency benchmarking**
  - Build a benchmark harness (`go test -bench` + a load-test script) simulating concurrent streaming sessions.
  - Publish target numbers (added latency, requests/sec, memory footprint) in `docs/benchmarks.md`.
  - `internal/proxy/bench_test.go` (`go test -bench`, in-process) measures the full pipeline's CPU/alloc cost: ~0.57ms non-streaming, ~0.65ms streaming. `scripts/loadtest` (a standalone Go program, real sockets, real concurrent client connections) measured ~4,700-4,900 req/s at 50-200 concurrent clients with p99 latency 45-158ms and zero failed requests. Full numbers in `docs/benchmarks.md`.
- [x] **5.3 Failure modes & resilience**
  - Upstream provider timeout/error handling and clear error propagation to the caller.
  - Config validation errors fail fast and loud at startup (no silent misconfiguration).
  - Graceful degradation strategy if the NER component fails to load (documented, not silent).
  - Added a streaming idle-timeout watchdog (`config.StreamIdleTimeout` / `LOCKER_STREAM_IDLE_TIMEOUT`, default 90s): a stalled upstream stream is force-closed rather than holding a goroutine and client connection open forever. `config.Load`/`applyEnvOverrides` no longer silently swallow a malformed env var (bad duration, non-`true`/`false` bool, negative integer) — every one is now a hard error. NER "failing to load" has no failure mode by construction (no external dependency); documented in the `internal/pii/ner` package doc comment that any future backend that *can* fail to load must report it through `pii.NewEngine`'s existing `(*Engine, error)` return rather than silently degrading.
- [x] **5.4 Security hardening**
  - Ensure API keys and unmasked PII are never written to logs by default.
  - Add a `SECURITY.md` threat-model section specific to a proxy that temporarily holds sensitive data in memory.
  - Verified by test (`internal/proxy/security_test.go`), not just convention: captures real log output across a full request lifecycle (including an upstream-failure error path) and asserts neither a provider API key nor a raw PII value ever appears in it. `SECURITY.md` now has a Threat Model section covering API key handling, the request-scoped PII memory window, logging guarantees, and what's explicitly out of scope (memory-dump-level attacks, TLS MITM, multi-tenant isolation).
- [x] **5.5 Deliverable** — documented, reproducible benchmark results and a resilience test suite (chaos-style tests: upstream down, malformed input, oversized payloads) passing in CI.
  - `internal/proxy/resilience_test.go` covers: upstream down, malformed request JSON, oversized payload, malformed upstream response, and a real-HTTP-connection regression test for a genuine bug this phase's load testing found (see below). All run as part of the existing `go test ./...` CI job — no new CI wiring needed.
  - **Bug found and fixed during this phase**: `scripts/loadtest` (real sockets) immediately surfaced "wrote more than the declared Content-Length" / connection resets under load — invisible to `httptest.NewRecorder()`-based unit tests, which don't enforce HTTP wire-protocol framing. Cause: the non-streaming response path copied the upstream's `Content-Length` header and called `WriteHeader` *before* unmasking the body, but unmasking changes the body's byte length (e.g. `[EMAIL_1]` → `jean.dupont@example.com`). Fixed by deferring `Content-Length`/`WriteHeader` until the final, unmasked body is known (and stripping `Content-Length` entirely for streaming, letting the server chunk it). This is exactly the kind of bug Phase 5's real-process load testing exists to catch — not left as a known issue.

---

## Phase 6 — Multi-Provider Support

**Goal:** extend beyond OpenAI to match the "2-3 providers" promise in `initial.md`.

- [x] **6.1 Anthropic adapter** — request/response translation to/from the OpenAI-compatible surface exposed by Locker.
  - Anthropic's Messages API genuinely differs in shape (top-level `system` instead of a `system`-role message, required `max_tokens`, `content` as an array of typed blocks, and a `content_block_delta`/`message_delta`/`message_stop` streaming event model instead of `choices[].delta.content`) — this needed real translation, not just routing. `internal/providers/anthropic.go` + `anthropic_stream.go` implement `TranslateRequest`/`TranslateResponse`/`NewStreamTranslator`; PII masking (`internal/pii`) required zero changes since it only ever sees Locker's own OpenAI-compatible shape, before/after translation.
- [x] **6.2 Mistral adapter** — same pattern.
  - Mistral's chat completions API is already OpenAI-shaped, so `internal/providers/mistral.go` only implements routing/auth and embeds `passthroughTranslation` for identity request/response/stream translation — no reshaping needed.
- [x] **6.3 Provider abstraction cleanup**
  - Extract a clean `Provider` interface so community contributors can add new providers without touching core proxy logic.
  - Document "how to add a provider" as a contributor guide.
  - `Provider` now covers translation too (`TranslateRequest`/`TranslateResponse`/`NewStreamTranslator`), and `config.knownProviders` generalizes the per-provider env var/default-base-URL wiring that used to be OpenAI-only. Adding a provider touches exactly two places (`providers.New`'s switch, `config.knownProviders`) plus its own new file — `internal/proxy` needed no changes for Mistral and only a call-site wiring change (already done) to route through translation for any future provider. Contributor guide added to `CONTRIBUTING.md` ("Adding a provider").
- [x] **6.4 Deliverable** — the same client code (unmodified, just swapping a `model` field or config entry) can route to OpenAI, Anthropic, or Mistral through Locker.
  - Proven at three levels: unit tests per provider (`internal/providers/*_test.go`, 87.5% coverage), end-to-end proxy tests against a fake upstream speaking each provider's *native* shape (`internal/proxy/anthropic_e2e_test.go`, covering non-streaming, streaming, and Mistral's identity path — PII masked going out, restored coming back, in every case), and re-validated against the compiled binary with a real Anthropic-shaped fake upstream (correct `/v1/messages` path, `x-api-key`/`anthropic-version` headers, `system` extraction, `max_tokens` default, and native SSE events correctly translated into OpenAI-shaped chunks ending in `[DONE]`).

---

## Phase 7 — Distribution & Packaging

**Goal:** deliver every artifact promised in `initial.md`, ready to run in under 5 minutes.

- [ ] **7.1 Docker image**
  - Multi-stage `Dockerfile` (minimal final image, distroless or scratch-based where possible).
  - Publish to Docker Hub and GHCR, tagged by semver + `latest`.
  - Automate build/publish in CI on tagged releases.
- [ ] **7.2 Helm chart**
  - `/charts/locker` with configurable values (replica count, resources, config mounting via `ConfigMap`/`Secret`).
  - Chart-testing CI job (`helm lint`, `helm template`, install against a kind/k3d cluster in CI).
  - Publish chart to a Helm repo (GitHub Pages-hosted index or ArtifactHub listing).
- [ ] **7.3 Single compiled binaries**
  - Cross-compile for Linux/macOS/Windows (amd64/arm64) via `goreleaser`.
  - Attach to GitHub Releases automatically on tag push.
- [ ] **7.4 CLI polish**
  - `locker start --config config.yaml`, `locker version`, `locker validate-config`.
  - Shell completion (bash/zsh) as a nice-to-have.
- [ ] **7.5 Kubernetes manifests (non-Helm alternative)**
  - Plain YAML manifests in `/deploy/k8s` for teams that don't use Helm.
- [ ] **7.6 Deliverable** — a new user can go from zero to a running, PII-masking proxy via `docker run`, `helm install`, or downloading a binary — each path documented with a copy-pasteable quickstart.

---

## Phase 8 — Documentation & Community Readiness

**Goal:** make the project genuinely adoptable by outside developers, not just runnable by the author.

- [ ] **8.1 User-facing documentation**
  - Quickstart (3 deployment paths from Phase 7).
  - Configuration reference (full `config.yaml` schema documented).
  - "How masking works" explainer (the 3-layer pipeline, in plain terms, for a developer audience — not the sales framing used in `initial.md`).
  - Provider setup guides (OpenAI/Anthropic/Mistral API key setup).
- [ ] **8.2 Contributor documentation**
  - Architecture overview diagram (proxy → RegEx → NER → symbolic layer → provider → restore).
  - "Adding a provider" and "adding a RegEx/rule" guides.
  - Issue templates, PR template, good-first-issue labeling.
- [ ] **8.3 Examples repository/folder**
  - Example integrations: LangChain, a raw `curl` example, a Python/JS SDK snippet just changing `BASE_URL`.
- [ ] **8.4 Public benchmarks page**
  - Publish latency/throughput numbers from Phase 5 in a readable format (table + short methodology).
- [ ] **8.5 Deliverable** — documentation site (even a simple static docs folder rendered via GitHub Pages/MkDocs) live and linked from the README.

---

## Phase 9 — Public Launch (v1.0)

**Goal:** ship a stable, versioned v1.0 and start building an open source user base.

- [ ] **9.1 Release criteria checklist**
  - All Phase 1–7 deliverables complete and green in CI.
  - No known critical bugs in the masking pipeline (false negatives on the core PII types are treated as release blockers).
  - Semantic versioning policy documented (`SemVer`, changelog process).
- [ ] **9.2 v1.0.0 tag & release**
  - Final Docker image, Helm chart, binaries, CLI all published under the `v1.0.0` tag.
  - Release notes summarizing the full feature set.
- [ ] **9.3 Launch communication**
  - GitHub repo polish (topics, social preview image, badges: build status, license, Docker pulls).
  - Launch posts (Hacker News, Reddit r/selfhosted / r/devops, relevant Discord/Slack communities) — positioned strictly as the open source PII-masking proxy, not the commercial story.
- [ ] **9.4 Feedback loop**
  - Set up a lightweight triage process for GitHub issues.
  - Track adoption signals (GitHub stars, Docker pulls, issues opened) to inform Phase 10 priorities.
- [ ] **9.5 Deliverable** — public v1.0.0 release, announced, with a working feedback channel.

---

## Phase 10 — Post-Launch Iteration (Ongoing)

**Goal:** keep the open source core healthy and useful as a foundation, independent of the future commercial layer.

- [ ] **10.1** Triage and fix community-reported false positives/negatives in the masking pipeline.
- [ ] **10.2** Expand provider adapters based on community requests (Bedrock, Vertex AI, local models via Ollama/vLLM — noting local-model support is explicitly listed as a "divers" integration point in the broader product vision, even though hosted by the client, not this repo).
- [ ] **10.3** Performance tuning based on real-world benchmark reports from users.
- [ ] **10.4** Periodic RegEx/rule-set updates (new PII formats, new locales beyond French SIREN/SIRET — e.g., other national ID formats) as community contributions.
- [ ] **10.5** Maintain a clear, publicly documented boundary between "what's free forever in Community Edition" and "what requires the commercial Enterprise layer," so the open source core keeps its own reason to exist per the *Open-Core* strategy in `initial.md`.

---

## Summary Timeline (indicative, not committed dates)

| Phase | Focus | Depends on |
|---|---|---|
| 0 | Repo foundations | — |
| 1 | Minimal pass-through proxy | 0 |
| 2 | RegEx + validation masking | 1 |
| 3 | Local NER | 2 |
| 4 | Symbolic "Fourmi" layer | 3 |
| 5 | Streaming/perf/hardening | 4 |
| 6 | Multi-provider support | 1 (can run in parallel with 2–5) |
| 7 | Distribution & packaging | 5, 6 |
| 8 | Documentation & community | 7 |
| 9 | Public v1.0 launch | 8 |
| 10 | Post-launch iteration | 9 |
