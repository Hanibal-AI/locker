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

## Adding a provider or a detection rule

See the dedicated guides in `docs/` (added starting Phase 8) once they exist. Until then, open an issue describing what you'd like to add.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting security issues

Do not open a public issue for security vulnerabilities — see [SECURITY.md](SECURITY.md).
