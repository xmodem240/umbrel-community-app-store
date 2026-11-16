# Debian slim (glibc) rather than Alpine: the zeromq native bindings have
# far more reliable prebuilt binaries for glibc than for musl across both
# amd64 and arm64, which keeps this buildable without compiling libzmq
# from source in CI.
#
# Pinned by digest - the multi-arch index, so it resolves on amd64 and arm64
# alike - like everything the store pins: the same Dockerfile then builds from
# the same base on every run. The price is that base-image updates (Node and
# Debian security fixes) arrive only when this digest is moved on, together
# with the tag, as part of a release.

# The peer map (map/, Go): one static binary with its page, the world map and
# the region and operator tables embedded. Built on the build machine's own
# architecture and cross-compiled, so the arm64 image needs no QEMU for it.
# Pinned by digest like the Node image; map/toolchain_test.go keeps go.mod in step.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS map-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY map/go.mod ./
COPY map/*.go ./
COPY map/geo ./geo
COPY map/asn ./asn
COPY map/torexit ./torexit
COPY map/web ./web
COPY package.json /tmp/package.json
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.Version=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' /tmp/package.json | head -1)" -o /out/peer-map .

FROM node:22-bookworm-slim@sha256:5ec23b7c622b00547753bc6ffd78085dd868a68aeb41c8e0a1418f033e13d96d AS builder
WORKDIR /app

# Only needed as a fallback if a native dependency has no prebuilt binary
# for the target platform (e.g. under qemu emulation during multi-arch CI).
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 make g++ \
    && rm -rf /var/lib/apt/lists/*

# `npm ci` rather than `npm install`, and no glob on the lockfile: this image
# is published under a tag+digest that the app store pins as "exactly this
# artifact", so the build has to actually be reproducible. The old
# `package-lock.json*` glob meant a missing lockfile still produced a happy
# build from whatever the registry served that day.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund

# Both native packages ship more than this image can ever load. zeromq
# includes a prebuilt binary for every platform it supports - Windows, macOS,
# and musl alongside glibc - and better-sqlite3 ships the SQLite C sources it
# was compiled from, which are a build input and nothing else. Since 13.0
# better-sqlite3 also bundles a prebuilt binary per platform (prebuilds/), so
# the same platforms go there too. This image is
# Debian (glibc) Linux and compiles nothing at runtime, so all of that is
# weight that can never be used.
#
# Only what provably cannot match is removed. Both linux/glibc architectures
# stay, because the same Dockerfile builds amd64 and arm64.
#
# Measured on a full install: node_modules 43MB -> 20MB, with the whole test
# suite still passing against the pruned tree.
RUN rm -rf node_modules/zeromq/build/win32 \
           node_modules/zeromq/build/darwin \
           node_modules/better-sqlite3/deps \
           node_modules/better-sqlite3/src \
           node_modules/better-sqlite3/prebuilds/darwin-* \
           node_modules/better-sqlite3/prebuilds/win32-* \
           node_modules/better-sqlite3/prebuilds/linuxmusl-* \
    && find node_modules/zeromq/build/linux -type d -name 'musl-*' -prune -exec rm -rf {} +

COPY src ./src

FROM node:22-bookworm-slim@sha256:5ec23b7c622b00547753bc6ffd78085dd868a68aeb41c8e0a1418f033e13d96d
WORKDIR /app
ENV NODE_ENV=production

COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/src ./src
COPY package.json ./
# Started by the dashboard itself, on loopback (src/lib/peer-map.js).
COPY --from=map-build /out/peer-map /app/peer-map

# The stock node:* image already ships a non-root "node" user at uid/gid
# 1000, matching what Umbrel expects to own ${APP_DATA_DIR}.
USER node

# Overridden per compose service (dashboard-server.js / peer-profiler.js /
# relay-profiler.js / stratum-race.js) via `command:`.
CMD ["node", "src/dashboard-server.js"]
