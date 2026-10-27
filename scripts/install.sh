#!/usr/bin/env bash
#
# keepwarm installer / uninstaller.
#
# Installs the keepwarm binary to /usr/local/bin and a systemd unit to
# /etc/systemd/system, then enables and starts the service.
#
# Binary source resolution (first match wins):
#   1. a prebuilt binary in ./dist or ./ (a local checkout / CI artifact)
#   2. built from source with `go` if a local checkout + Go are present
#   3. downloaded from the GitHub release (no Go required) — checksum verified
#
# So it works three ways:
#   # from a clone:
#   sudo ./scripts/install.sh
#   # one-liner (downloads the latest release binary, no Go needed):
#   curl -fsSL https://raw.githubusercontent.com/0xarkstar/keepwarm/main/scripts/install.sh | sudo bash
#   # uninstall:
#   sudo ./scripts/install.sh --uninstall
#
# Env:
#   KEEPWARM_VERSION=v0.1.0   pin a release tag (default: latest)
#
set -euo pipefail

REPO="0xarkstar/keepwarm"
BIN_NAME="keepwarm"
BIN_DEST="/usr/local/bin/${BIN_NAME}"
UNIT_NAME="keepwarm.service"
UNIT_DEST="/etc/systemd/system/${UNIT_NAME}"
VERSION="${KEEPWARM_VERSION:-latest}"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/main"

# Resolve the repo root from this script's location, tolerating `curl | bash`
# (where BASH_SOURCE is not a real path) without tripping `set -e`.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"
if [ -n "${SCRIPT_DIR}" ]; then
  REPO_ROOT="$(cd "${SCRIPT_DIR}/.." 2>/dev/null && pwd || true)"
else
  REPO_ROOT=""
fi

log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m==>\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "this command needs root. Re-run with sudo."
  fi
}

require_systemd() {
  command -v systemctl >/dev/null 2>&1 || die "systemctl not found; this installer targets systemd Linux."
}

# Map uname -m to the Go arch name we publish.
detect_arch() {
  local m
  m="$(uname -m)"
  case "${m}" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "unsupported architecture: ${m}" ;;
  esac
}

# fetch URL DEST — download with curl or wget.
fetch() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "${dest}" "${url}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${dest}" "${url}"
  else
    die "need curl or wget to download ${url}"
  fi
}

# Best-effort SHA-256 verification of a downloaded binary against the release
# SHA256SUMS file. Skipped (with a warning) if no sha256 tool is available.
verify_checksum() {
  local file="$1" asset="$2" base_url="$3"
  local sumtool
  if command -v sha256sum >/dev/null 2>&1; then
    sumtool="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    sumtool="shasum -a 256"
  else
    warn "no sha256 tool found; skipping checksum verification"
    return 0
  fi
  local sums
  sums="$(mktemp)"
  if ! fetch "${base_url}/SHA256SUMS" "${sums}" 2>/dev/null; then
    warn "could not fetch SHA256SUMS; skipping checksum verification"
    rm -f "${sums}"
    return 0
  fi
  local want got
  want="$(grep " ${asset}\$" "${sums}" | awk '{print $1}' | head -1)"
  rm -f "${sums}"
  [ -n "${want}" ] || { warn "no checksum entry for ${asset}; skipping"; return 0; }
  got="$(${sumtool} "${file}" | awk '{print $1}')"
  [ "${want}" = "${got}" ] || die "checksum mismatch for ${asset} (want ${want}, got ${got})"
  log "checksum verified (${asset})"
}

# Echo a path to an executable keepwarm binary for ${arch}, acquiring it from a
# local artifact, a source build, or the GitHub release — in that order.
locate_binary() {
  local arch="$1"

  # 1. local prebuilt binary (clone / CI artifact)
  if [ -n "${REPO_ROOT}" ]; then
    local c
    for c in "${REPO_ROOT}/dist/${BIN_NAME}-linux-${arch}" "${REPO_ROOT}/dist/${BIN_NAME}" "${REPO_ROOT}/${BIN_NAME}"; do
      if [ -x "${c}" ]; then echo "${c}"; return 0; fi
    done
  fi

  # 2. build from source if this is a checkout and Go is available
  if [ -n "${REPO_ROOT}" ] && [ -f "${REPO_ROOT}/go.mod" ] && command -v go >/dev/null 2>&1; then
    warn "building from source with go..."
    local out="${REPO_ROOT}/dist/${BIN_NAME}-linux-${arch}"
    mkdir -p "${REPO_ROOT}/dist"
    ( cd "${REPO_ROOT}" && GOOS=linux GOARCH="${arch}" go build -ldflags "-s -w" -o "${out}" ./ ) >&2
    echo "${out}"
    return 0
  fi

  # 3. download the published release binary (no Go required)
  local asset="${BIN_NAME}-linux-${arch}" url base
  if [ "${VERSION}" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
    base="https://github.com/${REPO}/releases/latest/download"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
    base="https://github.com/${REPO}/releases/download/${VERSION}"
  fi
  local tmp
  tmp="$(mktemp)"
  warn "downloading ${asset} (${VERSION}) from GitHub releases..."
  fetch "${url}" "${tmp}" || die "download failed: ${url} (is the release published?)"
  verify_checksum "${tmp}" "${asset}" "${base}"
  chmod +x "${tmp}"
  echo "${tmp}"
}

# Echo a path to the systemd unit — local checkout if present, else download.
locate_unit() {
  if [ -n "${REPO_ROOT}" ] && [ -f "${REPO_ROOT}/${UNIT_NAME}" ]; then
    echo "${REPO_ROOT}/${UNIT_NAME}"
    return 0
  fi
  local tmp
  tmp="$(mktemp)"
  fetch "${RAW_BASE}/${UNIT_NAME}" "${tmp}" || die "could not fetch ${UNIT_NAME}"
  echo "${tmp}"
}

install_keepwarm() {
  require_root
  require_systemd
  local arch src unit
  arch="$(detect_arch)"
  log "architecture: ${arch}   version: ${VERSION}"

  src="$(locate_binary "${arch}")"
  log "installing binary -> ${BIN_DEST}"
  install -m 0755 "${src}" "${BIN_DEST}"

  unit="$(locate_unit)"
  log "installing systemd unit -> ${UNIT_DEST}"
  install -m 0644 "${unit}" "${UNIT_DEST}"

  log "reloading systemd and enabling service"
  systemctl daemon-reload
  systemctl enable --now "${UNIT_NAME}"

  log "done. Current status:"
  systemctl status "${UNIT_NAME}" --no-pager || true
  echo
  log "Logs:   journalctl -u ${UNIT_NAME} -f"
  log "Verify: ${BIN_NAME} -status"
}

uninstall_keepwarm() {
  require_root
  require_systemd

  if systemctl list-unit-files | grep -q "^${UNIT_NAME}"; then
    log "stopping and disabling ${UNIT_NAME}"
    systemctl disable --now "${UNIT_NAME}" || true
  else
    warn "service ${UNIT_NAME} not registered; skipping stop/disable"
  fi

  if [ -f "${UNIT_DEST}" ]; then
    log "removing ${UNIT_DEST}"
    rm -f "${UNIT_DEST}"
    systemctl daemon-reload
  fi

  if [ -f "${BIN_DEST}" ]; then
    log "removing ${BIN_DEST}"
    rm -f "${BIN_DEST}"
  fi

  log "keepwarm uninstalled."
}

main() {
  case "${1:-}" in
    --uninstall|-u) uninstall_keepwarm ;;
    ""|--install)   install_keepwarm ;;
    -h|--help)
      cat <<EOF
keepwarm installer

Usage:
  sudo ./scripts/install.sh              install + enable + start the service
  sudo ./scripts/install.sh --uninstall  stop + disable + remove
  curl -fsSL ${RAW_BASE}/scripts/install.sh | sudo bash

Env:
  KEEPWARM_VERSION=v0.1.0   pin a release tag (default: latest)
EOF
      ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
}

main "$@"
