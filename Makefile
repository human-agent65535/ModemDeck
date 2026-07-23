SHELL := /bin/sh

GO_IMAGE ?= golang:1.26.3-bookworm
AGENT_NAME ?= modemdeck-agent
IMAGE ?= modemdeck
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)
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
COMPOSE_ADMIN_PASSWORD_FILE ?= /dev/null
HOST_UID := $(shell id -u)
HOST_GID := $(shell id -g)

export MODEMDECK_AGENT_GID

ROOT_GO = docker run --rm \
	-e GOTOOLCHAIN=local \
	--mount type=volume,source=modemdeck-root-go-mod,target=/go/pkg/mod \
	--mount type=volume,source=modemdeck-root-go-build,target=/root/.cache/go-build \
	-v "$(CURDIR):/workspace" \
	-w /workspace \
	$(GO_IMAGE)

ROOT_PACKAGES = ./cmd/... ./internal/...

AGENT_GO = docker run --rm \
	-e GOTOOLCHAIN=local \
	--mount type=volume,source=modemdeck-agent-go-mod,target=/go/pkg/mod \
	--mount type=volume,source=modemdeck-agent-go-build,target=/root/.cache/go-build \
	-v "$(CURDIR):/workspace" \
	-w /workspace/agent \
	$(GO_IMAGE)

.PHONY: all build app-build agent-build image check root-test agent-test \
	root-vet agent-vet web-install web-check web-build compose-config \
	compose-up compose-down compose-logs prepare-data install-agent clean

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
	$(AGENT_GO) sh -ec 'CGO_ENABLED=0 GOOS=$(TARGETOS) GOARCH=$(TARGETARCH) \
		go build -mod=readonly -trimpath -buildvcs=false \
		-ldflags="-s -w -X main.version=$(VERSION)" \
		-o /workspace/$(AGENT_OUT) $(AGENT_MAIN); \
		chown $(HOST_UID):$(HOST_GID) /workspace/$(AGENT_OUT)'

image:
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

check: root-test agent-test root-vet agent-vet web-check compose-config

root-test:
	$(ROOT_GO) go test -mod=readonly $(ROOT_PACKAGES)

agent-test:
	$(AGENT_GO) go test -mod=readonly ./...

root-vet:
	$(ROOT_GO) go vet -mod=readonly $(ROOT_PACKAGES)

agent-vet:
	$(AGENT_GO) go vet -mod=readonly ./...

web-install:
	npm ci --prefix web --no-audit --no-fund

web-check: web-install
	npm run typecheck --prefix web
	npm run lint --prefix web
	npm run build --prefix web

web-build: web-install
	npm run build --prefix web

compose-config:
	MODEMDECK_BUILD_DATE="$(BUILD_DATE)" \
	MODEMDECK_VCS_REF="$(VCS_REF)" \
	MODEMDECK_AGENT_GID="$(MODEMDECK_AGENT_GID)" \
	MODEMDECK_ADMIN_PASSWORD_FILE="$(COMPOSE_ADMIN_PASSWORD_FILE)" \
	docker compose config --quiet

prepare-data:
	sudo env MODEMDECK_UID="$(MODEMDECK_UID)" MODEMDECK_GID="$(MODEMDECK_GID)" \
		./scripts/prepare-modemdeck-data.sh "$(CURDIR)/data"

install-agent: agent-build
	sudo env MODEMDECK_AGENT_GID="$(MODEMDECK_AGENT_GID)" \
		./scripts/install-modemdeck-agent.sh "$(CURDIR)/$(AGENT_OUT)"

compose-up:
	docker compose up --detach --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs --follow modemdeck

clean:
	rm -rf "$(DIST_DIR)" web/dist
