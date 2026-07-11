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

FROM golang:1.26.5-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN test -n "${VERSION}" \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /veil ./cmd/veil

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -h /var/lib/veil veil \
    && mkdir -p /etc/veil /var/lib/veil \
    && chown -R veil:veil /etc/veil /var/lib/veil

COPY --from=builder /veil /usr/local/bin/veil

ENV VEIL_STATE_PATH=/var/lib/veil/state.json \
    VEIL_APPLY_ROOT=/etc/veil \
    VEIL_KEY_PATH=/etc/veil/state.key

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD veil status --listen http://127.0.0.1:2096 --json || exit 1

USER veil
EXPOSE 2096

ENTRYPOINT ["veil"]
CMD ["serve"]
