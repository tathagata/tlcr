# syntax=docker/dockerfile:1

# GO_VERSION is passed in by `make` (read from go.mod), so the toolchain
# version lives in exactly one place. Override with:
#   make build GO_VERSION=1.24
ARG GO_VERSION=1.27.1

FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG GOOS=linux
ARG GOARCH=amd64
ARG OUTPUT=coderead

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/${OUTPUT} .

# Default target: `docker build --target bin --output type=local,dest=./dist .`
# copies the built binary straight to the host without a container ever
# running on it.
FROM scratch AS bin
COPY --from=builder /out/ /
