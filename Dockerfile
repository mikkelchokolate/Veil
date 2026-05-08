# syntax=docker/dockerfile:1
# Veil — management panel for NaiveProxy, Hysteria2, and Mieru
#
# Build:
#   docker build -t veil .
#
# Run (local-only panel, no auth):
#   docker run -d --name veil --network host \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     veil serve
#
# Run (public panel with auth and hidden base path):
#   docker run -d --name veil -p 2096:2096 \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     -v /etc/systemd/system:/host-systemd:ro \
#     -e VEIL_API_TOKEN=your-secret-token \
#     -e VEIL_WEB_BASE_PATH=/secret-panel/ \
#     veil serve --listen 0.0.0.0:2096 --auth-token your-secret-token --web-base-path /secret-panel/
#
# Run (auto Let's Encrypt TLS):
#   docker run -d --name veil -p 443:443 \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     -v /etc/systemd/system:/host-systemd:ro \
#     -e VEIL_API_TOKEN=your-secret-token \
#     -e VEIL_AUTO_TLS=1 \
#     veil serve --listen 0.0.0.0:443 --auth-token your-secret-token --auto-tls

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o /veil ./cmd/veil

FROM alpine:3.20

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
