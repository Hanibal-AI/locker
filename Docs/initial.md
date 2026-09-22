# Locker — Open Source Core (Community Edition)

## Scope

This document describes **only the open source core engine of Locker** — the self-hosted proxy itself. It explicitly does **not** cover the commercial/SaaS layer (Control Plane, admin dashboard, enterprise SSO, FinOps/billing, multi-tenant policy management, hosted offering, or the WebGL 3D observability UI). Those are separate, closed products built on top of this core and are out of scope here.

## What Locker is

Locker is an open source, self-hosted HTTP/HTTPS proxy that sits between an application (or a developer's own tooling) and LLM provider APIs (OpenAI, Gemini, Mistral, Anthropic, etc.). It intercepts outgoing prompts, detects and anonymizes sensitive data (PII, secrets, source code fragments) *before* they leave the local network, forwards the sanitized request to the selected LLM provider, and restores the original values in the response before returning it to the caller.

It is designed to be dropped into any existing stack with a single change: pointing `BASE_URL` at Locker instead of the LLM provider directly.

## Goals

- Be a fast, deterministic, drop-in replacement proxy for LLM API calls.
- Detect and mask PII and sensitive data entirely locally — no extra LLM call, no added third-party cost, no hallucination risk.
- Be trivial to deploy: a single container or binary, configured via YAML or environment variables.
- Support streaming responses (SSE) without breaking the anonymization pipeline.
- Stay small, auditable, and dependency-light.

## Non-goals (explicitly out of scope for the open source core)

- No admin UI or dashboard for managing rules across an organization.
- No enterprise identity integration (SAML/OIDC/SSO) or multi-tenant policy management.
- No billing, FinOps, or per-department budget tracking.
- No hosted/managed offering.
- No WebGL 3D observability dashboard — the core only *emits* structured data; visualizing it is a separate, commercial concern.

## Core functionality

### 1. Transparent proxy

- Exposes an API compatible with the OpenAI Chat Completions spec (`/v1/chat/completions`), so any existing client, SDK, or tool (LangChain, Cursor, custom scripts) can point at Locker by changing only its `BASE_URL` — no application code changes required.
- Forwards requests to a small set of LLM providers out of the box (OpenAI, Anthropic, Mistral — extensible to others).
- Supports streaming responses (Server-Sent Events), with enough buffering to detect and mask PII that spans multiple tokens without destroying the streaming experience for the end user.

### 2. PII detection & anonymization pipeline

A layered, fully local, deterministic pipeline. No external LLM call is used for detection — this keeps latency near-zero and detection cost at zero:

1. **RegEx + validation formulas** — fast structural matching for well-formed data (emails, IBAN, credit card numbers validated with the Luhn algorithm, French SIREN/SIRET numbers), reducing false positives compared to naive RegEx.
2. **Local Named Entity Recognition (NER)** — a lightweight, locally-run model identifies unstructured entities (person names, organizations, locations, dates) that RegEx alone cannot reliably catch, with no network call involved.
3. **Symbolic reasoning layer ("Fourmi" ontology)** — an explicit, rule-based, graph-oriented engine (not a neural network) that maps recognized entities onto a small ontology (Action, Actor, Resource, Risk, Link, etc.). It is used to:
   - understand what role a detected entity plays in the sentence, so masking preserves sentence structure and semantic relevance for the downstream LLM (rather than blindly stripping tokens);
   - apply deterministic rule matrices (action × data sensitivity, actor × permitted scope, pattern-based prompt-injection/jailbreak detection) to qualify the risk of a request;
   - emit a compact, typed JSON graph payload (nodes/edges) describing the qualification of the prompt. The open source core only needs to *produce* this JSON — consuming and visualizing it is a separate, commercial concern.
   - Runs in-process, in Go, in low single-digit milliseconds, with fully deterministic, auditable output (every flag traces back to an explicit rule, never a probabilistic guess).

Masked values are replaced consistently within a single request/response cycle (e.g. `Jean Dupont` → `[ACTOR_1]`) and restored to their original values before the response reaches the caller, using an in-memory re-identification table scoped to that request.

### 3. Configuration

- Entirely file- or environment-variable driven: a single `config.yaml` (mounted into the container) or environment variables control provider API keys, which providers/models are allowed, and which masking rules are active.
- No database and no external control plane dependency required to run standalone.

### 4. Distribution

Published under a permissive open source license (Apache 2.0 or MIT), distributed as ready-to-run artifacts:

- **Docker image** (primary channel) on Docker Hub / GHCR — a single `docker run` starts the proxy.
- **Helm chart / Kubernetes manifests** for teams running on EKS, AKS, GKE, OpenShift, or on-prem clusters.
- **Single compiled binary** (Go) for Linux/macOS/Windows, published via GitHub Releases.
- **CLI** to start the proxy locally quickly, e.g. `locker start --config config.yaml`.

### 5. Design principles

- **Drop-in replacement**: changing `BASE_URL` is the only integration step required from any existing application.
- **Zero extra LLM calls**: all detection/qualification logic is rule-based and local; nothing is sent to a third party for the purpose of detection itself.
- **Deterministic & auditable**: every masking decision and every risk flag traces back to an explicit rule, not a probabilistic model — important for anyone who needs to explain *why* a request was flagged.
- **Small footprint, low latency**: written in Go specifically to keep the engine lightweight (target: single-digit-millisecond overhead for the detection pipeline) and to avoid the abstraction overhead of general-purpose multi-provider LLM SDKs.
- **Built from scratch, no upstream lock-in**: rather than wrapping an existing framework, the core is built independently to keep full control over architecture, license terms, and performance.

## Tech stack

- **Language**: Go.
- **Interfaces**: HTTP/HTTPS reverse proxy, OpenAI-compatible REST API, SSE streaming support.
- **Configuration**: YAML / environment variables.
- **No required external services** to run the core: the local NER model runs in-process or as a lightweight local sidecar; no database is required for basic operation.

## How this differs from existing open source PII tooling

Projects like Microsoft Presidio or LLM Guard offer PII detection/anonymization as a library that developers wire into their own stack. Locker's open source core packages that same kind of detection (RegEx + local NER) together with a symbolic, ontology-based reasoning layer that preserves sentence context and produces structured, auditable risk qualification — delivered as a self-contained, drop-in proxy rather than a library to integrate.
