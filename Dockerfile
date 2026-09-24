# syntax=docker/dockerfile:1
# Veil - management panel for NaiveProxy, Hysteria2, olcRTC, and Mieru
#
# Build:
#   docker build --build-arg VERSION=dev -t veil .
#
# Run (local-only panel, first-run session auth generated in mounted state):
#   docker run -d --name veil --network host \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     veil serve
#
# Prepare public panel session auth, then run with token and hidden base path:
#   docker run --rm \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     veil admin reset
#   docker run -d --name veil -p 2096:2096 \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     -e VEIL_API_TOKEN=your-secret-token \
#     -e VEIL_WEB_BASE_PATH=/secret-panel/ \
#     veil serve --listen 0.0.0.0:2096 --auth-token your-secret-token --web-base-path /secret-panel/
#
# Container installs provide Panel administration and staging. Live host
# promotion, systemd control, backup/key operations, and updates require the
# bare-metal veil-helper.socket; mounting host systemd paths is unsupported.
#
# Run (auto Let's Encrypt TLS):
#   docker run -d --name veil -p 443:443 \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     -e VEIL_API_TOKEN=your-secret-token \
#     -e VEIL_AUTO_TLS=1 \
#     veil serve --listen 0.0.0.0:443 --auth-token your-secret-token --auto-tls

ARG NODE_IMAGE=node:26.8.2-alpine@sha256:ef24c5053d50fdc3e4e56eb4e7ddb7861874ab0fdc797046ba897581deb8e868
ARG GO_IMAGE=golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125
ARG ALPINE_IMAGE=alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

FROM ${NODE_IMAGE} AS webbuilder

ARG NPM_VERSION=12.0.2
ARG PNPM_VERSION=12.4.1

# The Go binary embeds web/dist (see web/web.go); build the SPA first so the
# image ships the real panel UI.
RUN npm_config_update_notifier=false npm install --global --ignore-scripts --no-audit --no-fund \
      "npm@${NPM_VERSION}" "pnpm@${PNPM_VERSION}" \
    && npm --version \
    && pnpm --version \
    && rm -rf /root/.npm

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml web/.npmrc web/pnpm-workspace.yaml ./
COPY web/scripts/prepare_msw_worker.mjs ./scripts/
COPY web/public/mockServiceWorker.js ./public/
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM ${GO_IMAGE} AS builder

ARG GO_GODEBUG=http2client=0
ARG GO_GOPROXY=https://proxy.golang.org|direct
ENV GODEBUG=${GO_GODEBUG} \
    GOPROXY=${GO_GOPROXY}

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
COPY --from=webbuilder /web/dist ./web/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test -n "${VERSION}" \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /veil ./cmd/veil

FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -h /var/lib/veil veil \
    && mkdir -p /etc/veil /var/lib/veil \
    && chown -R veil:veil /etc/veil /var/lib/veil

COPY --from=builder /veil /usr/local/bin/veil
COPY --chmod=0755 packaging/docker/entrypoint.sh /usr/local/bin/veil-entrypoint

# No VEIL_CONTAINER_HEALTH_PATH ENV here: a packaged /var/lib/veil pin would
# override the VEIL_VAR_DIR-derived default for custom-root containers. The
# entrypoint exports the derived path for `veil serve`, and `veil healthcheck`
# derives the same path itself (issue #753).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD veil healthcheck || exit 1

USER veil
EXPOSE 2096

ENTRYPOINT ["/usr/local/bin/veil-entrypoint"]
CMD ["serve"]
