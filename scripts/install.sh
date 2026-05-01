#!/usr/bin/env bash
set -euo pipefail

REPO="veil-panel/veil"
PROFILE="default"
DOMAIN=""
EMAIL=""
PORT="443"

usage() {
  cat <<USAGE
Veil installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/veil-panel/veil/main/scripts/install.sh | bash
  bash install.sh --profile ru-recommended --domain example.com --email admin@example.com

Options:
  --profile NAME       default or ru-recommended
  --domain DOMAIN      domain used for ACME and client configs
  --email EMAIL        ACME email
  --port PORT          preferred shared TCP/UDP port, default 443
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) PROFILE="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ "${EUID}" -ne 0 ]]; then
  echo "Veil installer must be run as root." >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemd is required." >&2
  exit 1
fi

if [[ "$PROFILE" == "ru-recommended" && -z "$DOMAIN" ]]; then
  read -r -p "Domain for Veil/ACME: " DOMAIN
fi

if [[ "$PROFILE" == "ru-recommended" ]]; then
  echo "Selected profile: ru-recommended"
  echo "Preferred shared port: ${PORT}/tcp for NaiveProxy and ${PORT}/udp for Hysteria2"
  echo "Domain: ${DOMAIN:-not-set}"
  echo
  echo "This skeleton installer does not download binaries yet."
  echo "Next implementation step: release download, checksum verification, port probing, config rendering."
else
  echo "Selected profile: default"
  echo "This skeleton installer does not download binaries yet."
fi
