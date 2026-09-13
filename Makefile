# Docker-only dev workflow: no Go install required on the host.
#
#   make test      run `go test ./...`
#   make vet       run `go vet ./...`
#   make tidy      run `go mod tidy` (writes go.mod/go.sum back to the host)
#   make fmt       run `gofmt -l -w .`
#   make build     build ./dist/coderead for your host OS/arch
#   make cross     build linux (amd64/arm64), macOS (arm64/amd64) and Windows binaries into ./dist
#   make shell     drop into a shell in the build container
#   make clean     remove ./dist and the build/module caches
#
# GO_VERSION is read from go.mod so there is exactly one place that pins the
# toolchain. Bump it there (or override: `make test GO_VERSION=1.24`) to try
# a different version everywhere at once.

# Prefer the `toolchain` directive (exact patch pin) over the looser `go`
# minimum-version line, so the image always matches what `go.mod` actually
# pins Go builds to everywhere else (the official images run with
# GOTOOLCHAIN=local, so this is what decides the real patch version used).
GO_VERSION := $(shell awk '/^toolchain go/{sub("^toolchain go","",$$0); print $$0; found=1; exit} /^go /{v=$$2} END{if (!found && v!="") print v}' go.mod)
IMAGE      := golang:$(GO_VERSION)-bookworm

# Map the host's `uname` to Go's GOOS/GOARCH so `make build` produces a
# binary you can actually run on this machine, not always linux/amd64.
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
  HOST_GOOS := darwin
else ifeq ($(UNAME_S),Linux)
  HOST_GOOS := linux
else
  HOST_GOOS := $(UNAME_S)
endif

ifeq ($(UNAME_M),x86_64)
  HOST_GOARCH := amd64
else ifneq (,$(filter $(UNAME_M),arm64 aarch64))
  HOST_GOARCH := arm64
else
  HOST_GOARCH := $(UNAME_M)
endif

# Run an arbitrary command inside the pinned Go image, as your own host
# user (so files written back, like go.sum from `tidy`, aren't root-owned).
# Module/build caches live under ./.dockerbuild inside the project (already
# owned by you via the bind mount) instead of a root-owned named volume or
# anything under your real $HOME.
DOCKER_RUN := docker run --rm \
	-v "$(CURDIR)":/src -w /src \
	-u $(shell id -u):$(shell id -g) \
	-e HOME=/tmp \
	-e GOMODCACHE=/src/.dockerbuild/gomodcache \
	-e GOCACHE=/src/.dockerbuild/gocache \
	$(IMAGE)

.PHONY: test vet tidy fmt build cross shell clean

test:
	$(DOCKER_RUN) go test ./...

vet:
	$(DOCKER_RUN) go vet ./...

tidy:
	$(DOCKER_RUN) go mod tidy

fmt:
	$(DOCKER_RUN) gofmt -l -w -- *.go

build:
	docker buildx build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg GOOS=$(HOST_GOOS) \
		--build-arg GOARCH=$(HOST_GOARCH) \
		--target bin \
		--output type=local,dest=./dist \
		.
	@echo "Built ./dist/coderead for $(HOST_GOOS)/$(HOST_GOARCH)"

cross:
	docker buildx build --build-arg GO_VERSION=$(GO_VERSION) --build-arg GOOS=linux --build-arg GOARCH=amd64 --build-arg OUTPUT=coderead-linux-amd64 --target bin --output type=local,dest=./dist .
	docker buildx build --build-arg GO_VERSION=$(GO_VERSION) --build-arg GOOS=linux --build-arg GOARCH=arm64 --build-arg OUTPUT=coderead-linux-arm64 --target bin --output type=local,dest=./dist .
	docker buildx build --build-arg GO_VERSION=$(GO_VERSION) --build-arg GOOS=darwin --build-arg GOARCH=arm64 --build-arg OUTPUT=coderead-macos-arm64 --target bin --output type=local,dest=./dist .
	docker buildx build --build-arg GO_VERSION=$(GO_VERSION) --build-arg GOOS=darwin --build-arg GOARCH=amd64 --build-arg OUTPUT=coderead-macos-amd64 --target bin --output type=local,dest=./dist .
	docker buildx build --build-arg GO_VERSION=$(GO_VERSION) --build-arg GOOS=windows --build-arg GOARCH=amd64 --build-arg OUTPUT=coderead-windows-amd64.exe --target bin --output type=local,dest=./dist .

shell:
	$(DOCKER_RUN) bash

clean:
	rm -rf dist
	chmod -R u+w .dockerbuild 2>/dev/null || true
	rm -rf .dockerbuild
