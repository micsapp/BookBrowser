#!/usr/bin/env bash
# Build and publish the latest Linux x64 BookBrowser server binary to Droppy.
#
# The binary is rebuilt only when its embedded Git revision, reported version,
# or target platform does not match the current Git HEAD. SHA256SUMS is updated
# after the binary upload succeeds, and the uploaded binary is downloaded once
# to verify both its checksum and embedded build ID.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROFILE="${DROPPY_PROFILE:-tnas_d}"
REMOTE_DIR="${DROPPY_REMOTE_DIR:-/public/bookbrowser_cli}"
SHARE_URL="${DROPPY_SHARE_URL:-https://tnas_d.micsapp.com/s/bookbrowser_cli}"
BINARY="${BOOKBROWSER_BINARY:-$SCRIPT_DIR/build/BookBrowser}"
ASSET_NAME="${BOOKBROWSER_DROPPY_ASSET:-BookBrowser-server-linux-x64}"
FORCE=0

usage() {
    sed -n '2,8p' "$0"
    printf '\nUsage: %s [--force] [--profile NAME] [--remote-dir PATH] [--binary PATH]\n' "$0"
}

die() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --force)
            FORCE=1
            shift
            ;;
        --profile)
            [[ $# -ge 2 ]] || die "--profile requires a name"
            PROFILE="$2"
            shift 2
            ;;
        --remote-dir)
            [[ $# -ge 2 ]] || die "--remote-dir requires a path"
            REMOTE_DIR="$2"
            shift 2
            ;;
        --binary)
            [[ $# -ge 2 ]] || die "--binary requires a path"
            BINARY="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "unknown option: $1"
            ;;
    esac
done

command -v git >/dev/null 2>&1 || die "git is required"
command -v droppy_cli >/dev/null 2>&1 || die "droppy_cli is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

GO_BIN="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO_BIN" && -x /usr/local/go/bin/go ]]; then
    GO_BIN=/usr/local/go/bin/go
fi
[[ -n "$GO_BIN" ]] || die "go is required"

git -C "$SCRIPT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1 || \
    die "$SCRIPT_DIR is not a Git repository"
git -C "$SCRIPT_DIR" diff --quiet || \
    die "tracked source files have uncommitted changes; commit them before publishing"
git -C "$SCRIPT_DIR" diff --cached --quiet || \
    die "staged source files have uncommitted changes; commit them before publishing"

HEAD_FULL="$(git -C "$SCRIPT_DIR" rev-parse HEAD)"
HEAD_SHORT="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD)"
VERSION="droppy-$HEAD_SHORT"

binary_metadata() {
    "$GO_BIN" version -m "$1" 2>/dev/null || true
}

binary_setting() {
    local binary="$1" setting="$2"
    binary_metadata "$binary" | sed -n "s/^[[:space:]]*build[[:space:]]*$setting=//p" | head -1
}

binary_version() {
    "$1" --version 2>/dev/null | sed -n 's/^BookBrowser //p' | head -1
}

binary_is_current() {
    [[ -f "$BINARY" ]] || return 1
    [[ "$(binary_setting "$BINARY" vcs.revision)" == "$HEAD_FULL" ]] || return 1
    [[ "$(binary_version "$BINARY")" == "$VERSION" ]] || return 1
    [[ "$(binary_setting "$BINARY" GOOS)" == linux ]] || return 1
    [[ "$(binary_setting "$BINARY" GOARCH)" == amd64 ]] || return 1
}

build_binary() {
    local staged build_time
    mkdir -p "$(dirname "$BINARY")"
    staged="$BINARY.new.$$"
    build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    trap 'rm -f "$staged"' RETURN

    printf 'Building %s (build ID %s)...\n' "$BINARY" "$HEAD_SHORT"
    (
        cd "$SCRIPT_DIR"
        GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "$GO_BIN" build -trimpath \
            -ldflags "-s -w -X main.curversion=$VERSION -X main.buildID=$HEAD_SHORT -X main.buildTime=$build_time" \
            -o "$staged" .
    )
    chmod 0755 "$staged"
    mv -f "$staged" "$BINARY"
    trap - RETURN
}

if [[ $FORCE -eq 1 ]] || ! binary_is_current; then
    build_binary
else
    printf 'Binary is current (build ID %s); skipping build.\n' "$HEAD_SHORT"
fi

binary_is_current || die "compiled binary does not match HEAD $HEAD_FULL"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
STAGED_ASSET="$TMP_DIR/$ASSET_NAME"
REMOTE_MANIFEST="$TMP_DIR/SHA256SUMS.remote"
NEW_MANIFEST="$TMP_DIR/SHA256SUMS"
VERIFIED_ASSET="$TMP_DIR/verified-$ASSET_NAME"

cp "$BINARY" "$STAGED_ASSET"
chmod 0755 "$STAGED_ASSET"
LOCAL_SHA="$(sha256sum "$STAGED_ASSET" | awk '{print $1}')"

if ! droppy_cli --profile "$PROFILE" download "$REMOTE_DIR/SHA256SUMS" "$REMOTE_MANIFEST" >/dev/null; then
    : > "$REMOTE_MANIFEST"
fi

awk -v asset="$ASSET_NAME" -v sha="$LOCAL_SHA" '
    $2 == asset || $2 == "*" asset { next }
    NF { print }
    END { print sha "  " asset }
' "$REMOTE_MANIFEST" > "$NEW_MANIFEST"

printf 'Uploading %s to %s%s...\n' "$ASSET_NAME" "$PROFILE:" "$REMOTE_DIR"
droppy_cli --profile "$PROFILE" upload "$STAGED_ASSET" "$REMOTE_DIR"

printf 'Publishing updated SHA256SUMS...\n'
droppy_cli --profile "$PROFILE" upload "$NEW_MANIFEST" "$REMOTE_DIR"

printf 'Verifying uploaded binary...\n'
droppy_cli --profile "$PROFILE" download "$REMOTE_DIR/$ASSET_NAME" "$VERIFIED_ASSET" >/dev/null
chmod 0755 "$VERIFIED_ASSET"
REMOTE_SHA="$(sha256sum "$VERIFIED_ASSET" | awk '{print $1}')"
[[ "$REMOTE_SHA" == "$LOCAL_SHA" ]] || \
    die "uploaded checksum mismatch: local=$LOCAL_SHA remote=$REMOTE_SHA"
[[ "$(binary_setting "$VERIFIED_ASSET" vcs.revision)" == "$HEAD_FULL" ]] || \
    die "uploaded binary does not contain Git revision $HEAD_FULL"
[[ "$(binary_version "$VERIFIED_ASSET")" == "$VERSION" ]] || \
    die "uploaded binary does not report version $VERSION"

printf 'Uploaded and verified %s\n' "$SHARE_URL/$ASSET_NAME"
printf 'Build ID: %s\nSHA-256: %s\n' "$HEAD_SHORT" "$LOCAL_SHA"
