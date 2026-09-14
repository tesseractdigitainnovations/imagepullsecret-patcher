# syntax=docker/dockerfile:1

# The builder always runs on the native platform of the machine doing the build
# and cross-compiles for the target, which is far faster than emulating the
# target platform under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /src

# Download modules in their own layer so that they are cached until go.mod or
# go.sum changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
# linux/arm/v7 and friends: buildx passes the variant separately, and Go wants
# it as GOARM. Empty for every other platform, which Go treats as unset.
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_DATE=""

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build \
      -trimpath \
      -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.date=${BUILD_DATE}" \
      -o /out/imagepullsecret-patcher \
      ./cmd/imagepullsecret-patcher

# The static distroless image ships CA certificates and a nonroot user, and
# nothing else: no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot

# Image metadata is passed in by whoever builds the image: CI derives it from
# the repository with docker/metadata-action, and `make docker` derives it from
# the git remote. Nothing here is tied to a particular owner or registry.
ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_DATE=""
ARG IMAGE_TITLE="imagepullsecret-patcher"
ARG IMAGE_DESCRIPTION="Creates and patches imagePullSecrets onto service accounts in every Kubernetes namespace"
ARG IMAGE_SOURCE=""
ARG IMAGE_URL=""
ARG IMAGE_LICENSES="MIT"

LABEL org.opencontainers.image.title="${IMAGE_TITLE}" \
      org.opencontainers.image.description="${IMAGE_DESCRIPTION}" \
      org.opencontainers.image.source="${IMAGE_SOURCE}" \
      org.opencontainers.image.url="${IMAGE_URL}" \
      org.opencontainers.image.licenses="${IMAGE_LICENSES}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"

COPY --from=builder /out/imagepullsecret-patcher /usr/local/bin/imagepullsecret-patcher

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/imagepullsecret-patcher"]
