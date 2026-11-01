# Multi-stage build producing a tiny static keepalive image.
#
#   docker build -t keepalive .
#   docker run --rm keepalive -status
#
# To actually defend an instance from idle reclamation, run with enough memory
# headroom, e.g.:
#   docker run -d --name keepalive --restart unless-stopped keepalive -mem 30%
#
# Note: a container sees the host's /proc, so memory/CPU detection works, but
# the held memory counts against the container's cgroup limit — size -mem and
# any --memory limit accordingly.

# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /src

# No third-party deps, so just copy and build (no module download step needed).
COPY . .

ARG VERSION=docker
# CGO disabled => fully static binary that runs on scratch.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /out/keepalive ./

# ---- runtime stage ----
FROM scratch
COPY --from=build /out/keepalive /keepalive
# CA certs are needed only if the optional -net pinger (HTTPS) is enabled.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENTRYPOINT ["/keepalive"]
CMD ["-mem", "30%", "-interval", "30s"]
