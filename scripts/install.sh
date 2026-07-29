#!/bin/sh
set -eu

OFFICIAL_REPO="mikkelchokolate/Veil"
COSIGN_VERSION="v2.4.3"
COSIGN_AMD64_SHA256="caaad125acef1cb81d58dcdc454a1e429d09a750d1e9e2b3ed1aed8964454708"
COSIGN_ARM64_SHA256="bd0f9763bca54de88699c3656ade2f39c9a1c7a2916ff35601caf23a79be0629"
ISSUER="https://token.actions.githubusercontent.com"

for command in curl sha256sum tar sed awk uname mktemp python3 sudo; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Required command not found: $command" >&2
    exit 1
  }
done

work="$(mktemp -d)"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64"; cosign_sha256="$COSIGN_AMD64_SHA256" ;;
  aarch64|arm64) arch="arm64"; cosign_sha256="$COSIGN_ARM64_SHA256" ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac
[ "$os" = "linux" ] || { echo "Unsupported OS: $os" >&2; exit 1; }

cosign="$work/cosign"
curl -fsSLo "$cosign" "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-${arch}"
printf '%s  %s\n' "$cosign_sha256" "$cosign" | sha256sum -c - >/dev/null
chmod 0755 "$cosign"

release_json="$(curl -fsSL "https://api.github.com/repos/${OFFICIAL_REPO}/releases/latest")"
tag="$(printf '%s\n' "$release_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
case "$tag" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "Latest release has an invalid tag: $tag" >&2; exit 1 ;;
esac

base="https://github.com/${OFFICIAL_REPO}/releases/download/${tag}"
asset="veil_${os}_${arch}.tar.gz"
for name in \
  install-privileged.sh install-privileged.sh.bundle \
  checksums.txt checksums.txt.bundle \
  veil.provenance.json veil.provenance.json.bundle \
  "$asset"; do
  curl -fsSLo "$work/$name" "$base/$name"
done

identity="https://github.com/${OFFICIAL_REPO}/.github/workflows/release.yml@refs/tags/${tag}"
"$cosign" verify-blob --bundle "$work/install-privileged.sh.bundle" \
  --certificate-identity "$identity" --certificate-oidc-issuer "$ISSUER" \
  "$work/install-privileged.sh" >/dev/null
"$cosign" verify-blob --bundle "$work/checksums.txt.bundle" \
  --certificate-identity "$identity" --certificate-oidc-issuer "$ISSUER" \
  "$work/checksums.txt" >/dev/null
"$cosign" verify-blob --bundle "$work/veil.provenance.json.bundle" \
  --certificate-identity "$identity" --certificate-oidc-issuer "$ISSUER" \
  "$work/veil.provenance.json" >/dev/null

(
  cd "$work"
  count="$(awk -v asset="$asset" '$2 == asset { count++ } END { print count+0 }' checksums.txt)"
  [ "$count" -eq 1 ] || { echo "Expected exactly one checksum for $asset, got $count" >&2; exit 1; }
  awk -v asset="$asset" '$2 == asset { print }' checksums.txt | sha256sum -c - >/dev/null
)

ASSET="$asset" WORK="$work" REPOSITORY="$OFFICIAL_REPO" RELEASE_TAG="$tag" WORKFLOW_IDENTITY="$identity" python3 - <<'PY'
import hashlib, json, os

def digest(name):
    with open(os.path.join(os.environ["WORK"], name), "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()

with open(os.path.join(os.environ["WORK"], "veil.provenance.json"), encoding="utf-8") as handle:
    statement = json.load(handle)
if statement.get("_type") != "https://in-toto.io/Statement/v1":
    raise SystemExit("invalid in-toto statement type")
if statement.get("predicateType") != "https://slsa.dev/provenance/v1":
    raise SystemExit("invalid SLSA provenance predicate")
predicate = statement.get("predicate", {})
params = predicate.get("buildDefinition", {}).get("externalParameters", {})
if params.get("repository") != "https://github.com/" + os.environ["REPOSITORY"]:
    raise SystemExit("provenance repository mismatch")
if params.get("ref") != "refs/tags/" + os.environ["RELEASE_TAG"]:
    raise SystemExit("provenance tag mismatch")
if predicate.get("runDetails", {}).get("builder", {}).get("id") != os.environ["WORKFLOW_IDENTITY"]:
    raise SystemExit("provenance workflow identity mismatch")
subjects = {item.get("name"): item.get("digest", {}).get("sha256") for item in statement.get("subject", [])}
for name in (os.environ["ASSET"], "checksums.txt", "install-privileged.sh"):
    if subjects.get(name) != digest(name):
        raise SystemExit("provenance subject digest mismatch for " + name)
PY

tar -xzf "$work/$asset" -C "$work" veil
[ -f "$work/veil" ] && [ ! -L "$work/veil" ] || {
  echo "Verified archive did not contain a regular veil binary" >&2
  exit 1
}
archive_digest="$(sha256sum "$work/$asset" | awk '{print $1}')"
binary_digest="$(sha256sum "$work/veil" | awk '{print $1}')"
installer_digest="$(sha256sum "$work/install-privileged.sh" | awk '{print $1}')"

# No privileged process is started before every downloaded privileged payload
# above has passed signature, provenance, checksum, and archive-shape checks.
sudo env \
  VEIL_INSTALLER_SHA256="$installer_digest" \
  VEIL_VERIFIED_ARCHIVE_SHA256="$archive_digest" \
  VEIL_VERIFIED_BINARY_SHA256="$binary_digest" \
  bash "$work/install-privileged.sh" --local-bin "$work/veil" "$@"
