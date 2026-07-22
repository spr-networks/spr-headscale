# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
ARG ALPINE_REF=alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG UBUNTU_REF=ubuntu:24.04@sha256:4fbb8e6a8395de5a7550b33509421a2bafbc0aab6c06ba2cef9ebffbc7092d90
ARG NODE_REF=node:18@sha256:c6ae79e38498325db67193d391e6ec1d224d96c693a8a4d943498556716d3783
ARG CONTAINER_TEMPLATE_REF=ghcr.io/spr-networks/container_template@sha256:869ada7b121e9a0c552674042d32e801da3c4d04145638d9e722918c6377e65f
ARG SPR_KRUN_PLUGIN_REF=ghcr.io/spr-networks/spr-krun-plugin:latest
ARG SOURCE_DATE_EPOCH

FROM ${ALPINE_REF} AS cacerts

FROM ${UBUNTU_REF} AS builder
ENV DEBIAN_FRONTEND=noninteractive
ARG UBUNTU_SNAPSHOT=20260601T000000Z
ARG GO_VERSION=1.26.5
ARG GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
ARG GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49
ARG HEADSCALE_VERSION=v0.29.2
ARG HEADSCALE_COMMIT=8eea89488c642f3d5f617fab5493d5f51f6f4ad0
ARG TARGETARCH
COPY --from=cacerts /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN set -eux; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: noble noble-updates noble-security\nComponents: main restricted universe multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' "${UBUNTU_SNAPSHOT}" > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'APT::Install-Recommends "false";\nAcquire::Check-Valid-Until "false";\n' > /etc/apt/apt.conf.d/99reproducible
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git wget && rm -rf /var/lib/apt/lists/* /var/log/* /var/cache/ldconfig/aux-cache
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) GO_SHA256="${GO_SHA256_AMD64}";; \
      arm64) GO_SHA256="${GO_SHA256_ARM64}";; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1;; \
    esac; \
    wget -q "https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    echo "${GO_SHA256}  go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    rm "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"
ENV PATH="/usr/local/go/bin:${PATH}" GOTOOLCHAIN=local
# Build headscale from source, pinned to the release tag's full commit hash.
# The tag ref is fetched alongside the commit so Go's buildinfo stamps the
# release version; rev-parse enforces that the tag still points at the pin.
RUN set -eux; \
    mkdir /headscale-src; \
    cd /headscale-src; \
    git init -q; \
    git remote add origin https://github.com/juanfont/headscale.git; \
    git fetch -q --depth 1 origin "${HEADSCALE_COMMIT}" "refs/tags/${HEADSCALE_VERSION}:refs/tags/${HEADSCALE_VERSION}"; \
    git checkout -q "${HEADSCALE_COMMIT}"; \
    test "$(git rev-parse HEAD)" = "${HEADSCALE_COMMIT}"; \
    test "$(git rev-parse "${HEADSCALE_VERSION}^{commit}")" = "${HEADSCALE_COMMIT}"
RUN --mount=type=tmpfs,target=/root/go/ cd /headscale-src && go build -trimpath -ldflags "-s -w" -o /headscale ./cmd/headscale
WORKDIR /code
COPY code/ /code/
RUN --mount=type=tmpfs,target=/root/go/ go build -trimpath -ldflags "-s -w -X main.PinnedHeadscaleVersion=${HEADSCALE_VERSION}" -o /headscale_plugin /code/

FROM ${NODE_REF} AS builder-ui
WORKDIR /app
COPY frontend ./
RUN --mount=type=tmpfs,target=/root/.cache \
    --mount=type=tmpfs,target=/app/node_modules \
    yarn install --frozen-lockfile --network-timeout 86400000 && yarn run bundle

FROM ${SPR_KRUN_PLUGIN_REF}
ENV DEBIAN_FRONTEND=noninteractive
COPY scripts /scripts/
COPY --from=builder /headscale /usr/local/bin/headscale
COPY --from=builder /headscale_plugin /
COPY --from=builder-ui /app/build/ /ui/

CMD ["/scripts/startup.sh"]
