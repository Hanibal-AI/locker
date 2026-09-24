# Contributing to Locker

Thanks for considering a contribution. Locker is the open source core proxy — see [Docs/initial.md](Docs/initial.md) for scope and [Docs/roadmap.md](Docs/roadmap.md) for what's currently in progress.

## Getting started

```bash
git clone https://github.com/Hanibal-AI/locker.git
cd locker
cp config.example.yaml config.yaml
export OPENAI_API_KEY=sk-...
make build
make run
curl http://localhost:8080/healthz
```

Requires Go (version pinned in `.go-version`). Locker can also run without a `config.yaml` at all — see `internal/config/config.go` for the full list of `LOCKER_*` / `OPENAI_*` environment variables.

## Development workflow

- `make build` — compile the `locker` binary into `./bin`.
- `make test` — run the test suite.
- `make lint` — run `golangci-lint` (must pass before opening a PR).
- `make run` — run the proxy locally with default config.

## Project layout

- `cmd/locker` — CLI entrypoint.
- `internal/proxy` — reverse proxy engine.
- `internal/providers` — LLM provider adapters.
- `internal/pii` — PII detection/anonymization pipeline.
- `internal/config` — configuration loading.
- `charts/locker` — Helm chart.
- `deploy` — Docker Compose examples, Kubernetes manifests.

## Submitting a change

1. Open an issue first for anything non-trivial, so the approach can be discussed before code is written.
2. Keep PRs focused — one logical change per PR.
3. Add or update tests for any behavior change.
4. Make sure `make lint` and `make test` pass locally.
5. Describe *why* the change is needed in the PR description, not just what changed.

## Adding a provider

Locker always exposes one OpenAI-compatible surface to clients (`/v1/chat/completions`) regardless of which upstream provider is active. A provider adapter's whole job is translating between that shape and the upstream's native one — `internal/pii` (masking/unmasking) never needs to know a provider exists, and never changes when you add one.

Implement `providers.Provider` (`internal/providers/providers.go`):

```go
type Provider interface {
    Name() string
    Target(path string) string
    Authenticate(req *http.Request)
    TranslateRequest(body []byte) ([]byte, error)
    TranslateResponse(body []byte) ([]byte, error)
    NewStreamTranslator() StreamTranslator
}
```

Two cases:

- **The provider's native API is already OpenAI-shaped** (message roles, `choices[].message.content`, SSE `choices[].delta.content` streaming) — e.g. Mistral. Embed `passthroughTranslation` and you only need to write `Name`, `Target`, and `Authenticate`. See `internal/providers/mistral.go` for the full pattern (it's ~25 lines).
- **It isn't** — e.g. Anthropic's Messages API uses a top-level `system` field instead of a `system`-role message, requires `max_tokens`, and streams `content_block_delta`/`message_delta`/`message_stop` events instead of `choices[].delta.content`. Here you implement real translation: `TranslateRequest`/`TranslateResponse` reshape the non-streaming JSON, and `NewStreamTranslator` returns a `StreamTranslator` (mirrors `pii.SSEUnmasker`'s `Feed`/`Flush` shape) that converts native SSE frames into OpenAI-shaped ones. See `internal/providers/anthropic.go` and `anthropic_stream.go` for the full pattern, and their test files for the level of test coverage expected (request translation, response translation, every native streaming event type, a frame split across two reads).

Then:

1. Register it in `providers.New`'s switch statement.
2. Add its default base URL and `<NAME>_API_KEY` / `<NAME>_BASE_URL` env var names to `knownProviders` in `internal/config/config.go`.
3. Add an end-to-end test in `internal/proxy` against a fake upstream speaking the provider's *native* shape (not OpenAI's) — see `internal/proxy/anthropic_e2e_test.go` for the pattern: assert the fake upstream receives the translated+masked native request, and the client gets back an OpenAI-shaped, PII-restored response.
4. Add a commented-out example block to `config.example.yaml`.

## Adding a PII detection rule

A custom RegEx rule needs no code change — see the `pii.custom_rules` example in `config.example.yaml`. For a new *built-in* rule (its own validation formula, like Luhn or the IBAN checksum), see `internal/pii/rules.go` and `internal/pii/validation.go`, and add true-positive/false-positive-trap tests following the pattern in `internal/pii/engine_test.go`.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting security issues

Do not open a public issue for security vulnerabilities — see [SECURITY.md](SECURITY.md).
