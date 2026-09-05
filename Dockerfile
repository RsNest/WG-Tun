# Canonical build: Docker Engine + Buildx. Host Go is optional.
#   docker build --target test .
#   docker build --target controller -t ghcr.io/rsnest/transitforge-controller:local .
#   docker build --target agent -t ghcr.io/rsnest/transitforge-agent:local .
#   docker build --target cli -t ghcr.io/rsnest/transitforge-cli:local .
#
# Published platform for this phase: linux/amd64.

ARG GO_IMAGE=golang:1.26.8-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS deps
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

FROM deps AS src
COPY . .

FROM src AS test
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    sh -ec 'unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "gofmt needed:"; echo "$unformatted"; exit 1; fi; go vet ./...; go test ./...'

FROM src AS build
ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    sh -ec 'ldflags="-s -w -X transitforge/internal/version.Version=${VERSION} -X transitforge/internal/version.Commit=${COMMIT}"; \
      mkdir -p /out; \
      go build -trimpath -ldflags "$ldflags" -o /out/transitforge-controller ./cmd/controller; \
      go build -trimpath -ldflags "$ldflags" -o /out/transitforge-agent ./cmd/agent; \
      go build -trimpath -ldflags "$ldflags" -o /out/transitforge ./cmd/transitforge'

ARG RUNTIME_IMAGE=debian:bookworm-slim
FROM ${RUNTIME_IMAGE} AS runtime-base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /data /etc/transitforge /run/transitforge

ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/TransitForge
LABEL org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.licenses="UNLICENSED"

FROM runtime-base AS controller
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/TransitForge
LABEL org.opencontainers.image.title="TransitForge controller" \
      org.opencontainers.image.description="TransitForge desired-state controller (TLS API + SQLite)" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"
RUN apt-get update \
 && apt-get install -y --no-install-recommends passwd util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 65532 --home /data --shell /usr/sbin/nologin transitforge \
 && mkdir -p /data /data/certs /etc/transitforge \
 && chown transitforge:transitforge /data /data/certs
COPY --from=build /out/transitforge-controller /usr/local/bin/transitforge-controller
COPY configs/docker-controller.yaml /etc/transitforge/controller.yaml
COPY scripts/controller-entrypoint.sh /usr/local/bin/controller-entrypoint.sh
RUN chmod 0755 /usr/local/bin/controller-entrypoint.sh /usr/local/bin/transitforge-controller \
 && chmod 0644 /etc/transitforge/controller.yaml
EXPOSE 8443
VOLUME ["/data"]
HEALTHCHECK --interval=3s --timeout=2s --start-period=10s --retries=10 \
  CMD ["/usr/local/bin/transitforge-controller", "healthcheck", "--url", "https://127.0.0.1:8443/readyz", "-k"]
ENTRYPOINT ["/usr/local/bin/controller-entrypoint.sh"]
CMD ["--config", "/etc/transitforge/controller.yaml"]

FROM runtime-base AS cli
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/TransitForge
LABEL org.opencontainers.image.title="TransitForge CLI" \
      org.opencontainers.image.description="TransitForge operator CLI and compose bootstrap helper" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"
# wget is used only by docker-bootstrap.sh to wait on /readyz.
RUN apt-get update \
 && apt-get install -y --no-install-recommends wget \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/transitforge /usr/local/bin/transitforge
COPY scripts/docker-bootstrap.sh /usr/local/bin/docker-bootstrap.sh
RUN chmod 0755 /usr/local/bin/transitforge /usr/local/bin/docker-bootstrap.sh
ENV TRANSITFORGE_CONTROLLER=https://controller:8443
ENTRYPOINT ["transitforge"]

FROM runtime-base AS agent
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/TransitForge
LABEL org.opencontainers.image.title="TransitForge agent" \
      org.opencontainers.image.description="TransitForge edge agent (dry-run by default; live overlay needs host net + caps)" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"
# Runtime tools taken from CommandRunner allowlist usage in firewall/wireguard/haproxy/health:
# ip, wg, iptables{,-save,-restore}, haproxy -c, ping.
# systemctl is intentionally omitted: SSH TUN units and HAProxy reload stay on the host.
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      iproute2 iptables iputils-ping wireguard-tools haproxy \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /etc/haproxy \
 && printf 'global\n    daemon\ndefaults\n    mode tcp\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n' \
      >/etc/haproxy/haproxy.cfg
COPY --from=build /out/transitforge-agent /usr/local/bin/transitforge-agent
COPY configs/docker-agent.yaml /etc/transitforge/agent.yaml
RUN chmod 0755 /usr/local/bin/transitforge-agent
EXPOSE 9101
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=10 \
  CMD ["/usr/local/bin/transitforge-agent", "healthcheck", "--url", "http://127.0.0.1:9101/healthz"]
ENTRYPOINT ["transitforge-agent", "--config", "/etc/transitforge/agent.yaml", "--insecure"]
