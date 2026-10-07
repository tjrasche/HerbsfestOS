SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
KUSTOMIZE ?= kustomize
KO ?= ko
OVERLAY ?= dev
KIND ?= kind
KUBECTL ?= kubectl
DOCKER ?= docker
KIND_CLUSTER_NAME ?= herbsfest

.PHONY: generate fmt test build run migrate render image resolve
generate:
	go tool templ generate

fmt:
	go tool templ fmt .
	gofmt -w cmd internal

test: generate
	go test ./...
	go vet ./...

build: generate
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/web ./cmd/web

run: generate
	AUTH_MODE=$${AUTH_MODE:-development} go run ./cmd/web

migrate:
	go run ./cmd/web -migrate

render:
	$(KUSTOMIZE) build config/overlays/$(OVERLAY)

image: generate
	@test -n "$(KO_DOCKER_REPO)" || (echo 'Set KO_DOCKER_REPO to your image registry'; exit 1)
	$(KO) build ./cmd/web

resolve: generate
	@test -n "$(KO_DOCKER_REPO)" || (echo 'Set KO_DOCKER_REPO to your image registry'; exit 1)
	mkdir -p dist
	$(KUSTOMIZE) build config/overlays/$(OVERLAY) | $(KO) resolve -f - > dist/$(OVERLAY).yaml

.PHONY: kind-up dev-deploy dev dev-forward kind-down
kind-up:
	@if ! $(KIND) get clusters | grep -Fxq '$(KIND_CLUSTER_NAME)'; then \
		$(KIND) create cluster --name '$(KIND_CLUSTER_NAME)' --config config/kind/cluster.yaml --wait 120s; \
	fi

dev-deploy: generate
	mkdir -p dist
	dev_platform=$$($(KUBECTL) --context 'kind-$(KIND_CLUSTER_NAME)' get nodes -o jsonpath='{.items[0].status.nodeInfo.operatingSystem}/{.items[0].status.nodeInfo.architecture}'); \
		test -n "$$dev_platform"; \
		$(DOCKER) pull --platform="$$dev_platform" postgres:17.6-alpine; \
		$(DOCKER) image save --platform="$$dev_platform" --output dist/postgres.tar postgres:17.6-alpine; \
		$(KIND) load image-archive dist/postgres.tar --name '$(KIND_CLUSTER_NAME)'; \
		$(KUSTOMIZE) build config/overlays/dev | KO_DOCKER_REPO=kind.local KIND_CLUSTER_NAME='$(KIND_CLUSTER_NAME)' $(KO) resolve --platform="$$dev_platform" -f - > dist/dev.yaml
	$(KUBECTL) --context 'kind-$(KIND_CLUSTER_NAME)' apply -f dist/dev.yaml
	$(KUBECTL) --context 'kind-$(KIND_CLUSTER_NAME)' -n herbsfest-dev rollout status statefulset/postgres --timeout=180s
	$(KUBECTL) --context 'kind-$(KIND_CLUSTER_NAME)' -n herbsfest-dev rollout status deployment/herbsfest --timeout=180s

dev: kind-up
	$(MAKE) dev-deploy

dev-forward:
	$(KUBECTL) --context 'kind-$(KIND_CLUSTER_NAME)' -n herbsfest-dev port-forward service/herbsfest 8080:80

kind-down:
	$(KIND) delete cluster --name '$(KIND_CLUSTER_NAME)'
