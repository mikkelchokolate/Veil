# syntax=docker/dockerfile:1
# Veil — management panel for NaiveProxy and Hysteria2
#
# Build:
#   docker build -t veil .
#
# Run (local-only panel, no auth):
#   docker run -d --name veil --network host \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     veil serve
#
# Run (public panel with auth):
#   docker run -d --name veil -p 2096:2096 \
#     -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
#     -v /etc/systemd/system:/host-systemd:ro \
#     -e VEIL_API_TOKEN=your-secret-token \
#     veil serve --listen 0.0.0.0:2096 --auth-token your-secret-token

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o /veil ./cmd/veil

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -h /var/lib/veil veil

COPY --from=builder /veil /usr/local/bin/veil

ENV VEIL_STATE_PATH=/var/lib/veil/state.json \
    VEIL_APPLY_ROOT=/etc/veil \
    VEIL_KEY_PATH=/etc/veil/state.key

USER veil
EXPOSE 2096

ENTRYPOINT ["veil"]
CMD ["serve"]
