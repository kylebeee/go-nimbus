FROM ubuntu:24.04 AS builder

ARG GO_VERSION="1.25.3"
ARG TARGETARCH

ADD https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz /go.tar.gz

ENV HOME="/node" DEBIAN_FRONTEND="noninteractive" GOPATH="/dist"

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    apt-utils \
    bsdmainutils \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/* && \
    \
    tar -C /usr/local -xzf /go.tar.gz && \
    rm -rf /go.tar.gz

ENV PATH="/usr/local/go/bin:${GOPATH}/bin:${PATH}"

# Copy source tree and build from local source.
COPY . /src
WORKDIR /src

RUN ./scripts/configure_dev.sh && \
    BUILD_NUMBER="" make build

# Keep only the binaries we need (same set as install.sh).
RUN cd /dist/bin && \
    rm -vrf $(ls | grep -v -E '^(algocfg|algod|algokey|diagcfg|goal|kmd|msgpacktool|node_exporter|tealdbg|update\.sh|updater|COPYING)$') ; true

# Copy run files (templates, run.sh, genesis, etc.)
COPY ./docker/files/run/ /dist/files/run/
COPY ./installer/genesis /dist/files/run/genesis
COPY ./installer/config.json.example /dist/files/run/config.json.example

FROM debian:bookworm-20250630-slim AS final

ENV PATH="/node/bin:${PATH}" ALGOD_PORT="8080" KMD_PORT="7833" ALGORAND_DATA="/algod/data"

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p "$ALGORAND_DATA" && \
    groupadd --gid=999 --system algorand && \
    useradd --uid=999 --no-log-init --create-home --system --gid algorand algorand && \
    chown -R algorand:algorand /algod

COPY --chown=algorand:algorand --from=builder "/dist/bin/" "/node/bin/"
COPY --chown=algorand:algorand --from=builder "/dist/files/run/" "/node/run/"

# Expose relay API, nimbus API, KMD, gossip, and Prometheus metrics ports
EXPOSE $ALGOD_PORT 8081 $KMD_PORT 4160 9100

WORKDIR /algod

CMD ["/node/run/run.sh"]
