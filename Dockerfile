# syntax=docker/dockerfile:1
#
# Self-contained image: `docker build .` works with nothing else
# installed, for local development/testing. The release pipeline
# (Docs/roadmap.md Phase 7.1) uses a different, minimal Dockerfile
# instead (.goreleaser/Dockerfile) that copies goreleaser's own
# natively-cross-compiled binary — avoiding a slow emulated `go build`
# under QEMU for each non-native target architecture.

# Build stage: compiles a static binary.
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/locker ./cmd/locker

# Final stage: distroless, non-root, no shell — a static Go binary needs
# nothing else. ca-certificates are included in distroless/static so
# outbound HTTPS to LLM providers works out of the box.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/locker /usr/local/bin/locker
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/locker"]
CMD ["start"]
