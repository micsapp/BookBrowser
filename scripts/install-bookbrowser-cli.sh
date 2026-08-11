#!/usr/bin/env bash
# Install the BookBrowser remote CLI on Linux or macOS.
#
# Usage:
#   bash install-bookbrowser-cli.sh
#   bash install-bookbrowser-cli.sh --prefix DIR
#   bash install-bookbrowser-cli.sh --user
#   bash install-bookbrowser-cli.sh --uninstall
#
# One-liner:
#   curl -fsSL https://tnas_d.micsapp.com/s/bookbrowser_cli/install-bookbrowser-cli.sh | bash

set -Eeuo pipefail

BASE_URL="${BOOKBROWSER_CLI_BASE_URL:-https://tnas_d.micsapp.com/s/bookbrowser_cli}"
BIN_NAME="${BOOKBROWSER_CLI_BIN_NAME:-bookbrowser-cli}"
PREFIX=""
FORCE_USER=0
UNINSTALL=0
NEED_SUDO=0

if [[ -t 1 ]]; then
  BOLD="\033[1m"; RED="\033[31m"; GREEN="\033[32m"; YELLOW="\033[33m"; RESET="\033[0m"
else
  BOLD=""; RED=""; GREEN=""; YELLOW=""; RESET=""
fi

info() { printf "${BOLD}==>${RESET} %s\n" "$*"; }
warn() { printf "${YELLOW}WARN:${RESET} %s\n" "$*" >&2; }
die()  { printf "${RED}ERROR:${RESET} %s\n" "$*" >&2; exit 1; }
ok()   { printf "${GREEN}OK${RESET} %s\n" "$*"; }

usage() {
  cat <<'USAGE'
Install the BookBrowser remote CLI on Linux or macOS.

Usage:
  bash install-bookbrowser-cli.sh
  bash install-bookbrowser-cli.sh --prefix DIR
  bash install-bookbrowser-cli.sh --user
  bash install-bookbrowser-cli.sh --uninstall

Options:
  --prefix DIR  Install into DIR.
  --user        Install into ~/.local/bin without sudo.
  --uninstall   Remove bookbrowser-cli from the selected/common prefix.
  -h, --help    Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)
      [[ $# -ge 2 ]] || die "--prefix requires a directory"
      PREFIX="$2"; shift 2 ;;
    --user) FORCE_USER=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown argument: $1 (try --help)" ;;
  esac
done

detect_target() {
  local kernel arch
  kernel=$(uname -s 2>/dev/null || echo unknown)
  arch=$(uname -m 2>/dev/null || echo unknown)

  case "$kernel" in
    Linux) OS=linux ;;
    Darwin) OS=macos ;;
    CYGWIN*|MINGW*|MSYS*)
      die "Use ${BASE_URL}/bookbrowser-cli-windows-x64.exe on Windows." ;;
    *) die "Unsupported OS: $kernel" ;;
  esac

  case "$arch" in
    x86_64|amd64) ARCH=x64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "Unsupported architecture: $arch" ;;
  esac

  ASSET="bookbrowser-cli-${OS}-${ARCH}"
}

choose_prefix() {
  [[ -n "$PREFIX" ]] && return
  if [[ $FORCE_USER -eq 1 ]]; then
    PREFIX="$HOME/.local/bin"
    return
  fi

  local candidates=()
  if [[ "$OS" == macos && -d /opt/homebrew/bin ]]; then
    candidates+=(/opt/homebrew/bin)
  fi
  candidates+=(/usr/local/bin)

  local dir
  for dir in "${candidates[@]}"; do
    if [[ -d "$dir" && -w "$dir" ]]; then
      PREFIX="$dir"
      return
    fi
    if [[ -d "$dir" ]] && command -v sudo >/dev/null 2>&1; then
      PREFIX="$dir"
      NEED_SUDO=1
      return
    fi
  done
  PREFIX="$HOME/.local/bin"
}

ensure_prefix() {
  if [[ ! -d "$PREFIX" ]]; then
    info "Creating $PREFIX"
    if [[ -w "$(dirname "$PREFIX")" ]]; then
      mkdir -p "$PREFIX"
    elif command -v sudo >/dev/null 2>&1; then
      sudo mkdir -p "$PREFIX"
      NEED_SUDO=1
    else
      die "Cannot create $PREFIX. Try --user."
    fi
  fi
  if [[ ! -w "$PREFIX" ]]; then
    command -v sudo >/dev/null 2>&1 || die "$PREFIX is not writable. Try --user."
    NEED_SUDO=1
  fi
}

runp() {
  if [[ $NEED_SUDO -eq 1 ]]; then
    sudo "$@"
  else
    "$@"
  fi
}

download() {
  local url="$1" output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL --connect-timeout 15 --progress-bar "$url" -o "$output"
  elif command -v wget >/dev/null 2>&1; then
    wget --connect-timeout=15 --show-progress -qO "$output" "$url"
  else
    die "curl or wget is required."
  fi
}

verify_checksum() {
  local binary="$1" manifest="$2" expected actual
  expected=$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset {print $1; exit}' "$manifest")
  [[ -n "$expected" ]] || die "SHA256SUMS has no entry for $ASSET."
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$binary" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$binary" | awk '{print $1}')
  else
    die "sha256sum or shasum is required to verify the download."
  fi
  [[ "$actual" == "$expected" ]] || die "Checksum verification failed for $ASSET."
  ok "Checksum verified."
}

do_uninstall() {
  detect_target
  choose_prefix
  local target="$PREFIX/$BIN_NAME" dir
  if [[ ! -e "$target" ]]; then
    target=""
    for dir in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin"; do
      if [[ -e "$dir/$BIN_NAME" ]]; then
        target="$dir/$BIN_NAME"
        PREFIX="$dir"
        break
      fi
    done
  fi
  [[ -n "$target" ]] || { info "$BIN_NAME is not installed in a common location."; exit 0; }
  ensure_prefix
  info "Removing $target"
  runp rm -f "$target"
  ok "Removed."
}

main_install() {
  detect_target
  info "Detected ${OS}-${ARCH} ($ASSET)"
  choose_prefix
  ensure_prefix

  local binary manifest target staged size
  binary=$(mktemp -t "${BIN_NAME}.XXXXXX")
  manifest=$(mktemp -t "${BIN_NAME}.sha256.XXXXXX")
  trap 'rm -f "$binary" "$manifest"' EXIT

  info "Downloading ${BASE_URL}/${ASSET}"
  download "${BASE_URL}/${ASSET}" "$binary"
  download "${BASE_URL}/SHA256SUMS" "$manifest"

  size=$(stat -c '%s' "$binary" 2>/dev/null || stat -f '%z' "$binary" 2>/dev/null || echo 0)
  [[ "${size:-0}" -ge 1000000 ]] || die "Downloaded file is suspiciously small (${size:-0} bytes)."
  verify_checksum "$binary" "$manifest"

  target="$PREFIX/$BIN_NAME"
  staged="$PREFIX/.${BIN_NAME}.new.$$"
  info "Installing to $target"
  runp cp -f "$binary" "$staged"
  runp chmod 0755 "$staged"
  runp mv -f "$staged" "$target"

  if [[ "$OS" == macos ]]; then
    if command -v codesign >/dev/null 2>&1; then
      runp codesign --force --sign - "$target" 2>/dev/null || warn "Ad-hoc codesign failed."
    fi
    if command -v xattr >/dev/null 2>&1; then
      runp xattr -d com.apple.quarantine "$target" 2>/dev/null || true
    fi
  fi

  rm -f "$binary" "$manifest"
  trap - EXIT
  ok "Installed $BIN_NAME -> $target"

  if [[ ":$PATH:" != *":$PREFIX:"* ]]; then
    warn "$PREFIX is not on PATH. Add: export PATH=\"$PREFIX:\$PATH\""
  fi
  "$target" version

  cat <<NEXT

Next steps:
  $BIN_NAME login --method google-browser --url https://ebook.micstec.com
  $BIN_NAME whoami
  $BIN_NAME books list
NEXT
}

if [[ $UNINSTALL -eq 1 ]]; then
  do_uninstall
  exit 0
fi
main_install
