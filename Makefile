SHELL := /bin/sh

GO_IMAGE ?= golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
ROOT_TOOLCHAIN_IMAGE ?= modemdeck-root-toolchain:go1.26.5-opus1.3.1-3
ROOT_TOOLCHAIN_TARGET ?= go-toolchain
NODE_IMAGE ?= node:24.18.0-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d
GITLEAKS_IMAGE ?= ghcr.io/gitleaks/gitleaks:v8.30.1@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f
AGENT_NAME ?= modemdeck-agent
IMAGE ?= modemdeck
WEB_IMAGE ?= modemdeck-web
HARDWARE_IMAGE ?= modemdeck-hardware
RELEASE_VERSION ?= $(shell tr -d '\r\n' 2>/dev/null < VERSION)
VERSION ?= $(if $(RELEASE_VERSION),v$(RELEASE_VERSION),dev)
BUILD_DATE ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || printf '%s' unknown)
VCS_REF ?= $(shell git rev-parse HEAD 2>/dev/null || printf '%s' unknown)
DIST_DIR ?= dist
AGENT_OUT ?= $(DIST_DIR)/$(AGENT_NAME)
AGENT_MAIN ?= ./cmd/modemdeck-agent
TARGETOS ?= linux
TARGETARCH ?= amd64
MODEMDECK_UID ?= 10001
MODEMDECK_GID ?= 10001
MODEMDECK_AGENT_GID ?= 10002
COMPOSE_SETTINGS_KEY_FILE ?= /dev/null
COMPOSE_ASSIGNMENT_FILE ?= $(CURDIR)/deploy/advanced-assignment.example.json
COMPOSE_CLOUDFLARE_TOKEN_FILE ?= /dev/null
COMPOSE_CLOUDFLARE_PUBLIC_URL ?= https://modemdeck.example.com
HOST_UID := $(shell id -u)
HOST_GID := $(shell id -g)

export MODEMDECK_AGENT_GID

ROOT_GO = docker run --rm \
	-e GOTOOLCHAIN=local \
	--mount type=volume,source=modemdeck-root-go-mod,target=/go/pkg/mod \
	--mount type=volume,source=modemdeck-root-go-build,target=/root/.cache/go-build \
	--mount type=bind,source="$(CURDIR)",target=/workspace,readonly \
	-w /workspace \
	$(ROOT_TOOLCHAIN_IMAGE)

ROOT_PACKAGES = ./cmd/... ./internal/...

AGENT_GO_RO = docker run --rm \
	-e GOTOOLCHAIN=local \
	--mount type=volume,source=modemdeck-agent-go-mod,target=/go/pkg/mod \
	--mount type=volume,source=modemdeck-agent-go-build,target=/root/.cache/go-build \
	--mount type=bind,source="$(CURDIR)",target=/workspace,readonly \
	-w /workspace/agent \
	$(GO_IMAGE)

AGENT_GO_RW = docker run --rm \
	-e GOTOOLCHAIN=local \
	--mount type=volume,source=modemdeck-agent-go-mod,target=/go/pkg/mod \
	--mount type=volume,source=modemdeck-agent-go-build,target=/root/.cache/go-build \
	--mount type=bind,source="$(CURDIR)",target=/workspace \
	-w /workspace/agent \
	$(GO_IMAGE)

WEB_NODE_RO = docker run --rm \
	--mount type=bind,source="$(CURDIR)",target=/workspace,readonly \
	--mount type=volume,source=modemdeck-web-node-modules,target=/workspace/web/node_modules \
	--mount type=volume,source=modemdeck-web-npm-cache,target=/root/.npm \
	-w /workspace/web \
	$(NODE_IMAGE)

WEB_NODE_RW = docker run --rm \
	--mount type=bind,source="$(CURDIR)",target=/workspace,readonly \
	--mount type=bind,source="$(CURDIR)/web",target=/workspace/web \
	--mount type=volume,source=modemdeck-web-node-modules,target=/workspace/web/node_modules \
	--mount type=volume,source=modemdeck-web-npm-cache,target=/root/.npm \
	-w /workspace/web \
	$(NODE_IMAGE)

.PHONY: all build app-build agent-build image app-image web-image hardware-image check \
	hardware-check deployment-check root-toolchain root-test \
	agent-test root-vet agent-vet web-install web-test web-typecheck web-lint \
	web-check web-build compose-config dockerfile-check compose-up compose-down \
	compose-logs prepare-data install-check repository-check secret-scan clean

all: check build

build: app-build agent-build

app-build:
	mkdir -p "$(DIST_DIR)"
	docker build \
		--platform "$(TARGETOS)/$(TARGETARCH)" \
		--target modemdeck-artifact \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg VCS_REF="$(VCS_REF)" \
		--output type=local,dest="$(DIST_DIR)" \
		.

agent-build:
	mkdir -p "$(DIST_DIR)"
	$(AGENT_GO_RW) sh -ec 'CGO_ENABLED=0 GOOS=$(TARGETOS) GOARCH=$(TARGETARCH) \
		go build -mod=readonly -trimpath -buildvcs=false \
		-ldflags="-s -w -X main.version=$(VERSION)" \
		-o /workspace/$(AGENT_OUT) $(AGENT_MAIN); \
		chown $(HOST_UID):$(HOST_GID) /workspace/$(AGENT_OUT)'

image: app-image web-image hardware-image

app-image:
	docker build \
		--platform "$(TARGETOS)/$(TARGETARCH)" \
		--target runtime \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg VCS_REF="$(VCS_REF)" \
		--build-arg MODEMDECK_UID="$(MODEMDECK_UID)" \
		--build-arg MODEMDECK_GID="$(MODEMDECK_GID)" \
		-t "$(IMAGE):$(VERSION)" \
		.

web-image:
	docker build \
		--platform "$(TARGETOS)/$(TARGETARCH)" \
		--target web-runtime \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg VCS_REF="$(VCS_REF)" \
		-t "$(WEB_IMAGE):$(VERSION)" \
		.

hardware-image:
	docker build \
		--platform "$(TARGETOS)/$(TARGETARCH)" \
		--file hardware/Dockerfile \
		--target runtime \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg VCS_REF="$(VCS_REF)" \
		-t "$(HARDWARE_IMAGE):$(VERSION)" \
		.

check: repository-check root-test agent-test root-vet agent-vet web-check \
	hardware-check deployment-check compose-config dockerfile-check

repository-check:
	./scripts/check-repository-hygiene.sh

secret-scan:
	docker run --rm \
		--mount type=bind,source="$(CURDIR)",target=/repo,readonly \
		$(GITLEAKS_IMAGE) git /repo --no-banner --no-color --redact=100

root-toolchain:
	docker build \
		--target "$(ROOT_TOOLCHAIN_TARGET)" \
		--tag "$(ROOT_TOOLCHAIN_IMAGE)" \
		.

root-test: root-toolchain
	$(ROOT_GO) go test -mod=readonly $(ROOT_PACKAGES)

agent-test:
	$(AGENT_GO_RO) go test -mod=readonly ./...

root-vet: root-toolchain
	$(ROOT_GO) go vet -mod=readonly $(ROOT_PACKAGES)

agent-vet:
	$(AGENT_GO_RO) go vet -mod=readonly ./...

hardware-check:
	./hardware/tests/run.sh

deployment-check:
	./deploy/tests/run.sh

web-install:
	$(WEB_NODE_RO) npm ci --include=dev --no-audit --no-fund

web-test: web-install
	$(WEB_NODE_RO) npm test

web-typecheck: web-install
	$(WEB_NODE_RO) npm run typecheck
	$(WEB_NODE_RO) npm run typecheck:sidecar

web-lint: web-install
	$(WEB_NODE_RO) npm run lint

web-check: web-install
	$(WEB_NODE_RO) npm test
	$(WEB_NODE_RO) npm run typecheck
	$(WEB_NODE_RO) npm run typecheck:sidecar
	$(WEB_NODE_RO) npm run lint
	$(WEB_NODE_RW) npm run build

web-build: web-install
	$(WEB_NODE_RW) npm run build

compose-config:
	MODEMDECK_BUILD_DATE="$(BUILD_DATE)" \
	MODEMDECK_VCS_REF="$(VCS_REF)" \
	MODEMDECK_AGENT_GID="$(MODEMDECK_AGENT_GID)" \
	MODEMDECK_SETTINGS_KEY_FILE="$(COMPOSE_SETTINGS_KEY_FILE)" \
	docker compose config --quiet
	@if [ -f docker-compose.advanced.yml ]; then \
		MODEMDECK_BUILD_DATE="$(BUILD_DATE)" \
		MODEMDECK_VCS_REF="$(VCS_REF)" \
		MODEMDECK_AGENT_GID="$(MODEMDECK_AGENT_GID)" \
		MODEMDECK_SETTINGS_KEY_FILE="$(COMPOSE_SETTINGS_KEY_FILE)" \
		MODEMDECK_ASSIGNMENT_FILE="$(COMPOSE_ASSIGNMENT_FILE)" \
		docker compose \
			-f docker-compose.yml \
			-f docker-compose.advanced.yml \
			config --quiet; \
	fi
	@if [ -f docker-compose.cloudflare.yml ]; then \
		MODEMDECK_BUILD_DATE="$(BUILD_DATE)" \
		MODEMDECK_VCS_REF="$(VCS_REF)" \
		MODEMDECK_AGENT_GID="$(MODEMDECK_AGENT_GID)" \
		MODEMDECK_SETTINGS_KEY_FILE="$(COMPOSE_SETTINGS_KEY_FILE)" \
		MODEMDECK_CLOUDFLARE_TOKEN_FILE="$(COMPOSE_CLOUDFLARE_TOKEN_FILE)" \
		MODEMDECK_CLOUDFLARE_PUBLIC_URL="$(COMPOSE_CLOUDFLARE_PUBLIC_URL)" \
		docker compose \
			-f docker-compose.yml \
			-f docker-compose.cloudflare.yml \
			config --quiet; \
	fi

dockerfile-check:
	docker build --check .
	docker build --check --file hardware/Dockerfile .

prepare-data:
	sudo env MODEMDECK_UID="$(MODEMDECK_UID)" MODEMDECK_GID="$(MODEMDECK_GID)" \
		./scripts/prepare-modemdeck-data.sh "$(CURDIR)/data"

compose-up:
	docker compose up --detach --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs --follow hardware api modemdeck

install-check:
	./install.sh --check --allow-dirty

clean:
	rm -rf "$(DIST_DIR)" web/dist
