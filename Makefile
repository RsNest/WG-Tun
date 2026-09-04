# Optional wrapper. Canonical commands are plain `docker build` / `docker buildx bake`.
# Host Go is not required.

REGISTRY ?= ghcr.io/rsnest
VERSION ?= dev
COMMIT ?= unknown

.PHONY: test controller agent proxctl images smoke

test:
	docker build --target test --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

controller:
	docker build --target controller --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(REGISTRY)/wg-tun-controller:local .

agent:
	docker build --target agent --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(REGISTRY)/wg-tun-agent:local .

proxctl:
	docker build --target proxctl --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(REGISTRY)/wg-tun-proxctl:local .

images: controller agent proxctl

smoke: images
	CONTROLLER_IMAGE=$(REGISTRY)/wg-tun-controller:local \
	AGENT_IMAGE=$(REGISTRY)/wg-tun-agent:local \
	PROXCTL_IMAGE=$(REGISTRY)/wg-tun-proxctl:local \
	sh scripts/docker-ci-smoke.sh
