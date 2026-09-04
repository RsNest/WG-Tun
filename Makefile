# Optional wrapper. Canonical commands are plain `docker build` / `docker buildx bake`.
# Host Go is not required.

REGISTRY ?= ghcr.io/rsnest
VERSION ?= dev
COMMIT ?= unknown

.PHONY: test controller agent cli images smoke

test:
	docker build --target test --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

controller:
	docker build --target controller --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t transitforge-controller:local .

agent:
	docker build --target agent --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t transitforge-agent:local .

cli:
	docker build --target cli --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t transitforge-cli:local .

images: controller agent cli

smoke: images
	CONTROLLER_IMAGE=transitforge-controller:local \
	AGENT_IMAGE=transitforge-agent:local \
	CLI_IMAGE=transitforge-cli:local \
	sh scripts/docker-ci-smoke.sh
