# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Locker, please **do not** open a public GitHub issue.

Instead, report it privately using [GitHub's private vulnerability reporting](https://github.com/Hanibal-AI/locker/security/advisories/new) for this repository.

Please include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce (minimal repro case if possible).
- The version/commit of Locker affected.

We aim to acknowledge reports within 5 business days.

## Scope

This policy covers the Locker open source core proxy (this repository): the reverse proxy engine, provider adapters, and the PII detection/anonymization pipeline. It does not cover third-party services you connect Locker to (LLM providers, your own infrastructure).

## Supported Versions

Until a stable v1.0 is released, only the `main` branch is supported. Once v1.0 ships, this section will be updated with a supported-versions table.

## Threat Model

Locker's core job is to sit in the path of every prompt and response between your users and an LLM provider, which means it necessarily holds two categories of sensitive data in memory, however briefly: **provider API keys** (long-lived, configured once) and **unmasked PII** (short-lived, one request's worth at a time). This section describes what protections exist around that, and what's explicitly out of scope.

### API keys

- Read once at startup from `config.yaml` (with `${VAR}` expansion) or an environment variable (`OPENAI_API_KEY`, etc.) — see `internal/config`.
- Held in memory only, for the life of the process. Never written to disk by Locker itself.
- Injected into the `Authorization` header of the outbound request to the provider (`internal/providers`) and nowhere else — never logged, never included in an error message, never echoed back in a response.
- A config or provider error message never interpolates the key value itself (see `internal/config/config.go`'s `validate`), so a misconfigured deployment doesn't leak the key into its own startup logs.

### PII in memory

- The mapping between a placeholder (`[EMAIL_1]`) and the real value it stands in for (`internal/pii.Table`) is **request-scoped**: a fresh, empty table is created per request and becomes unreachable (eligible for garbage collection) once that request's response has been written. Nothing is written to disk, and no PII is retained across requests.
- The window during which the real value exists in memory is bounded by one request's lifetime — typically milliseconds to a few seconds for a streaming response.
- Neither the masked request sent upstream nor the placeholder tokens allow a passive observer of provider traffic to recover the original value: the mapping only ever exists inside the Locker process itself.

### Logging

- Locker logs operational events (upstream failures, stream stalls, body read/write errors) via the standard `log` package — see `internal/proxy`. These log lines are built from static messages, status codes, and Go error values; request/response **bodies are never included**, so neither raw PII nor a provider API key can end up in them by construction. This is enforced by tests (`internal/proxy/security_test.go`), not just by convention.
- If you add logging in a fork or PR, do not log full request/response bodies or header values at any log level — that's the one way this guarantee could regress.

### What's explicitly out of scope

- **Memory-level attacks**: a core dump, a process memory inspection tool (e.g. `ptrace`) run by someone with sufficient OS-level privilege on the host, or a Go runtime bug that leaks memory contents. Standard OS process isolation and least-privilege deployment (don't run Locker as root; don't grant `ptrace` capabilities to untrusted users on the host) are the relevant mitigations, and are your deployment's responsibility, not Locker's.
- **A compromised LLM provider or man-in-the-middle on that connection.** Locker uses standard TLS (Go's `net/http` default transport) to reach providers; it does not add its own certificate pinning.
- **Multi-tenant isolation, per-user access control, and audit logging of who sent what.** The open source core is a single-tenant proxy by design (see `Docs/initial.md`); that's Control Plane territory.
