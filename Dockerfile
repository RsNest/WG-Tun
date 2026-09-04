# Canonical build: Docker Engine + Buildx. Host Go is optional.
#   docker build --target test .
#   docker build --target controller -t ghcr.io/rsnest/wg-tun-controller:local .
#   docker build --target agent -t ghcr.io/rsnest/wg-tun-agent:local .
#   docker build --target proxctl -t ghcr.io/rsnest/wg-tun-proxctl:local .
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
    sh -ec 'ldflags="-s -w -X proxyctl/internal/version.Version=${VERSION} -X proxyctl/internal/version.Commit=${COMMIT}"; \
      mkdir -p /out; \
      go build -trimpath -ldflags "$ldflags" -o /out/proxyctl-controller ./cmd/controller; \
      go build -trimpath -ldflags "$ldflags" -o /out/proxyctl-agent ./cmd/agent; \
      go build -trimpath -ldflags "$ldflags" -o /out/proxctl ./cmd/proxctl'

ARG RUNTIME_IMAGE=debian:bookworm-slim
FROM ${RUNTIME_IMAGE} AS runtime-base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /data /etc/proxyctl /run/proxyctl

ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/WG-Tun
LABEL org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.licenses="UNLICENSED"

FROM runtime-base AS controller
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/WG-Tun
LABEL org.opencontainers.image.title="proxyctl controller" \
      org.opencontainers.image.description="proxyctl desired-state controller (TLS API + SQLite)" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"
RUN apt-get update \
 && apt-get install -y --no-install-recommends passwd util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 65532 --home /data --shell /usr/sbin/nologin proxyctl \
 && mkdir -p /data /data/certs /etc/proxyctl \
 && chown proxyctl:proxyctl /data /data/certs
COPY --from=build /out/proxyctl-controller /usr/local/bin/proxyctl-controller
COPY configs/docker-controller.yaml /etc/proxyctl/controller.yaml
COPY scripts/controller-entrypoint.sh /usr/local/bin/controller-entrypoint.sh
RUN chmod 0755 /usr/local/bin/controller-entrypoint.sh /usr/local/bin/proxyctl-controller \
 && chmod 0644 /etc/proxyctl/controller.yaml
EXPOSE 8443
VOLUME ["/data"]
HEALTHCHECK --interval=3s --timeout=2s --start-period=10s --retries=10 \
  CMD ["/usr/local/bin/proxyctl-controller", "healthcheck", "--url", "https://127.0.0.1:8443/readyz", "-k"]
ENTRYPOINT ["/usr/local/bin/controller-entrypoint.sh"]
CMD ["--config", "/etc/proxyctl/controller.yaml"]

FROM runtime-base AS proxctl
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/WG-Tun
LABEL org.opencontainers.image.title="proxctl" \
      org.opencontainers.image.description="proxyctl operator CLI and compose bootstrap helper" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"
# wget is used only by docker-bootstrap.sh to wait on /readyz.
RUN apt-get update \
 && apt-get install -y --no-install-recommends wget \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/proxctl /usr/local/bin/proxctl
COPY scripts/docker-bootstrap.sh /usr/local/bin/docker-bootstrap.sh
RUN chmod 0755 /usr/local/bin/proxctl /usr/local/bin/docker-bootstrap.sh
ENV PROXYCTL_CONTROLLER=https://controller:8443
ENTRYPOINT ["proxctl"]

FROM runtime-base AS agent
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
ARG SOURCE=https://github.com/RsNest/WG-Tun
LABEL org.opencontainers.image.title="proxyctl agent" \
      org.opencontainers.image.description="proxyctl edge agent (dry-run by default; live overlay needs host net + caps)" \
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
COPY --from=build /out/proxyctl-agent /usr/local/bin/proxyctl-agent
COPY configs/docker-agent.yaml /etc/proxyctl/agent.yaml
RUN chmod 0755 /usr/local/bin/proxyctl-agent
EXPOSE 9101
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=10 \
  CMD ["/usr/local/bin/proxyctl-agent", "healthcheck", "--url", "http://127.0.0.1:9101/healthz"]
ENTRYPOINT ["proxyctl-agent", "--config", "/etc/proxyctl/agent.yaml", "--insecure"]
