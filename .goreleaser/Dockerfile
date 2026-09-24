# syntax=docker/dockerfile:1
#
# Used only by the release pipeline (.goreleaser.yaml), never directly:
# goreleaser cross-compiles the `locker` binary for each target
# OS/arch natively (fast, no QEMU), then builds one minimal image per
# platform from that binary, joining them into a multi-arch manifest.
# For a self-contained local build, use the root /Dockerfile instead.
FROM gcr.io/distroless/static-debian12:nonroot
COPY locker /usr/local/bin/locker
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/locker"]
CMD ["start"]
