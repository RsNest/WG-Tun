# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/proxyctl-controller ./cmd/controller \
 && go build -trimpath -ldflags="-s -w" -o /out/proxyctl-agent ./cmd/agent \
 && go build -trimpath -ldflags="-s -w" -o /out/proxctl ./cmd/proxctl

FROM debian:bookworm-slim AS runtime-base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /data /etc/proxyctl /run/proxyctl

FROM runtime-base AS controller
COPY --from=build /out/proxyctl-controller /usr/local/bin/proxyctl-controller
COPY configs/docker-controller.yaml /etc/proxyctl/controller.yaml
EXPOSE 8443
VOLUME ["/data"]
HEALTHCHECK --interval=3s --timeout=2s --start-period=10s --retries=10 \
  CMD ["/usr/local/bin/proxyctl-controller", "healthcheck", "--url", "https://127.0.0.1:8443/readyz", "-k"]
ENTRYPOINT ["proxyctl-controller", "--config", "/etc/proxyctl/controller.yaml"]

FROM runtime-base AS proxctl
COPY --from=build /out/proxctl /usr/local/bin/proxctl
COPY scripts/docker-bootstrap.sh /usr/local/bin/docker-bootstrap.sh
RUN chmod +x /usr/local/bin/docker-bootstrap.sh
ENV PROXYCTL_CONTROLLER=https://controller:8443
ENTRYPOINT ["proxctl"]

FROM runtime-base AS agent
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      iproute2 iptables iputils-ping wireguard-tools haproxy \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /etc/haproxy \
 && printf 'global\n    daemon\ndefaults\n    mode tcp\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n' \
      >/etc/haproxy/haproxy.cfg
COPY --from=build /out/proxyctl-agent /usr/local/bin/proxyctl-agent
COPY configs/docker-agent.yaml /etc/proxyctl/agent.yaml
EXPOSE 9101
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=10 \
  CMD wget -q -O- http://127.0.0.1:9101/healthz >/dev/null || exit 1
ENTRYPOINT ["proxyctl-agent", "--config", "/etc/proxyctl/agent.yaml", "--insecure"]
