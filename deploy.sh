#!/usr/bin/env bash
#
# deploy.sh - build and deploy the BookBrowser web service and its TTS backend
# to a local server (default) or a remote server (--remote HOST).
#
# Usage:
#   ./deploy.sh [--remote HOST] [--force] [--skip-build] [--force-tts] [--help]
#
# What it does:
#   1. Loads deploy/.env, validates all required variables, and aborts with the
#      list of missing ones when any are absent.
#   2. Builds build/BookBrowser (linux) when the committed code has changed
#      since the last build (comparison is the embedded buildID == HEAD sha,
#      plus a regenerated packr asset check).
#   3. Deploys the web service on the target, installs the service unit, and
#      restarts it. The unit sources the validated .env document so every
#      needed variable reaches the process.
#   4. Detects whether the TTS backend is already deployed on the target and,
#      if it is missing, installs it (venv + script + unit + service start).
#   5. With --remote HOST, everything above is run on that host over SSH and
#      binaries/assets are transferred with scp.
#
# Requires: bash, git, go, packr, ssh/scp (remote only), sudo (aws11 mode).

set -euo pipefail

# ---------------------------------------------------------------------------
# Config / paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/deploy/.env"
ENV_EXAMPLE="$SCRIPT_DIR/deploy/.env.example"
BIN_LOCAL="$SCRIPT_DIR/build/BookBrowser"

# Variables that MUST be present and non-placeholder in deploy/.env.
REQUIRED_VARS=(BOOKBROWSER_GOOGLE_CLIENT_ID)

REMOTE=""
DO_FORCE=0
DO_FORCE_TTS=0
DO_SKIP_BUILD=0

# ---------------------------------------------------------------------------
# Help / args
# ---------------------------------------------------------------------------
usage() {
    sed -n '2,21p' "$SCRIPT_DIR/deploy.sh"
    echo
    echo "Options:"
    echo "  --remote HOST   deploy to HOST over SSH (default: local machine)"
    echo "  --force         rebuild the binary even if it is up to date"
    echo "  --skip-build    reuse the existing build/BookBrowser binary"
    echo "  --force-tts     (re)deploy the TTS backend even if it is running"
    echo "  -h, --help      show this help"
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --remote)
            [ "$#" -ge 2 ] || { echo "ERROR: --remote requires a hostname" >&2; exit 1; }
            REMOTE="$2"; shift 2 ;;
        --remote=*)
            REMOTE="${1#*=}"; shift ;;
        --force)
            DO_FORCE=1; shift ;;
        --skip-build)
            DO_SKIP_BUILD=1; shift ;;
        --force-tts)
            DO_FORCE_TTS=1; shift ;;
        -h|--help)
            usage; exit 0 ;;
        *)
            echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 1 ;;
    esac
done

# ---------------------------------------------------------------------------
# Target helpers (local vs remote over SSH)
# ---------------------------------------------------------------------------
SSH_HOST=""
if [ -n "$REMOTE" ]; then
    : "${DEPLOY_SSH_USER:=$(id -un)}"
    SSH_HOST="${DEPLOY_SSH_USER}@${REMOTE}"
fi

# run_cmd 'shell command' executes a command on the target.
run_cmd() {
    if [ -n "$SSH_HOST" ]; then
        ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_HOST" "$1"
    else
        bash -lc "$1"
    fi
}

# copy_to_target <local-file> <absolute-target-path>
copy_to_target() {
    local src="$1" dst="$2"
    if [ -n "$SSH_HOST" ]; then
        scp -o BatchMode=yes -q "$src" "$SSH_HOST:$dst"
    else
        cp "$src" "$dst"
    fi
}

# TARGET_HOME is the home directory of the login user on the target.
TARGET_HOME="$(run_cmd 'getent passwd $(id -u) | cut -d: -f6')"
TARGET_USER="$(basename "$TARGET_HOME")"
USER_UNIT_DIR="$TARGET_HOME/.config/systemd/user"

# ---------------------------------------------------------------------------
# .env handling
# ---------------------------------------------------------------------------
load_env() {
    local line key val
    if [ ! -f "$ENV_FILE" ]; then
        cp "$ENV_EXAMPLE" "$ENV_FILE"
        echo "INFO: created $ENV_FILE from $ENV_EXAMPLE" >&2
    fi
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            ''|'#'*) continue ;;
        esac
        key="${line%%=*}"
        val="${line#*=}"
        val="${val%\"}"; val="${val#\"}"
        val="${val%\'}"; val="${val#\'}"
        [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] && continue
        printf -v "$key" '%s' "$val"
        export "$key"
    done < "$ENV_FILE"
}

validate_env() {
    local missing=() v value
    for v in "${REQUIRED_VARS[@]}"; do
        value="${!v:-}"
        value="$(printf '%s' "$value" | tr -d '[:space:]')"
        if [ -z "$value" ] || \
           [[ "$value" == REPLACE* ]] || \
           [[ "$value" == *PLACEHOLDER* ]] || \
           [[ "$value" == *REPLACE_ME* ]]; then
            missing+=("$v")
        fi
    done
    if [ "${#missing[@]}" -gt 0 ]; then
        echo "ERROR: deploy/.env is missing required variable(s): ${missing[*]}" >&2
        echo "       Add them to $ENV_FILE (see $ENV_EXAMPLE)." >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Defaults / service mode
# ---------------------------------------------------------------------------
load_env
validate_env

: "${BOOK_DIR:=/home/mli/books}"
: "${DATA_DIR:=$BOOK_DIR/.bookbrowser}"
: "${WEB_ADDR:=127.0.0.1:8092}"
: "${TTS_LISTEN:=127.0.0.1}"
: "${TTS_PORT:=8094}"
: "${TTS_CACHE:=/var/cache/bookbrowser-tts}"
: "${TTS_VENV:=/home/mli/ttsvenv}"
: "${TTS_MAX_CONCURRENCY:=8}"
: "${TTS_PARALLEL_CHARS:=1200}"
: "${TTS_PARALLEL_SLOTS:=8}"
: "${TTS_SYNTH_TIMEOUT:=60}"
: "${GOARCH:=amd64}"
: "${DEPLOY_NGINX:=0}"
if [ -n "$SSH_HOST" ]; then
    : "${DEPLOY_SERVICE_MODE:=aws11}"
else
    : "${DEPLOY_SERVICE_MODE:=user}"
fi

# Locate build tools (builds happen locally even for remote targets).
ensure_tools() {
    case ":$PATH:" in
        *:/usr/local/go/bin:*) ;;
        *) [ -x /usr/local/go/bin/go ] && export PATH="/usr/local/go/bin:$PATH" ;;
    esac
    if ! command -v packr >/dev/null 2>&1 && [ -x "$HOME/go/bin/packr" ]; then
        export PATH="$HOME/go/bin:$PATH"
    fi
    command -v go >/dev/null 2>&1 || { echo "ERROR: go not found in PATH" >&2; exit 1; }
}

# ---------------------------------------------------------------------------
# Stage 1 - build the latest binary if the code changed
# ---------------------------------------------------------------------------
build_if_needed() {
    [ "$DO_SKIP_BUILD" = 1 ] && { echo "Skipping build (--skip-build)."; return; }
    ensure_tools

    local sha
    sha="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || echo dev)"
    local cur=""
    local need=0

    if [ ! -f "$BIN_LOCAL" ]; then
        need=1
    else
        cur="$(go version -m "$BIN_LOCAL" 2>/dev/null \
                | sed -n 's/.*main\.buildID=\([^" ]*\).*/\1/p' | head -1 || true)"
        if [ -z "$cur" ] || [ "$cur" != "$sha" ]; then
            need=1
        fi
    fi

    # If packr is available, regenerate the asset pack and rebuild when the
    # result no longer matches the committed pack (i.e. static files changed).
    if [ "$need" = 0 ] && [ "$DO_FORCE" = 0 ] && command -v packr >/dev/null 2>&1; then
        if ! ( cd "$SCRIPT_DIR/public" && packr -z >/dev/null 2>&1 ) \
           || ! git -C "$SCRIPT_DIR" diff --quiet -- public/a_public-packr.go; then
            need=1
        fi
    fi
    if [ "$need" = 0 ]; then
        echo "Build is up to date (buildID=$sha)."
        return
    fi

    echo "== Building BookBrowser (buildID=$sha) =="
    mkdir -p "$SCRIPT_DIR/build"
    if command -v packr >/dev/null 2>&1; then
        ( cd "$SCRIPT_DIR/public" && packr -z >/dev/null 2>&1 ) || \
            { echo "WARN: packr regeneration failed; continuing" >&2; }
    fi
    ( cd "$SCRIPT_DIR" && \
        GO111MODULE=on GOOS=linux GOARCH="$GOARCH" go build \
        -ldflags "-X main.curversion=deploy-$sha -X main.buildID=$sha \
                  -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        -o "$BIN_LOCAL" . )
    echo "Built $BIN_LOCAL"
}

# ---------------------------------------------------------------------------
# Stage 2 - deploy the web service
# ---------------------------------------------------------------------------
make_env_file() {
    local tmp_dotenv="$1"
    {
        echo "BOOKBROWSER_GOOGLE_CLIENT_ID=$BOOKBROWSER_GOOGLE_CLIENT_ID"
        echo "BOOKBROWSER_DATA_DIR=$DATA_DIR"
    } > "$tmp_dotenv"
}

deploy_web() {
    echo "== Deploying web service =="

    local tmp
    tmp="$(mktemp -d)"

    # The service unit and .env we push to the target.
    local web_bin_cmd
    if [ "$DEPLOY_SERVICE_MODE" = "aws11" ]; then
        # aws11 layout: runit launcher + static binary name in BOOK_DIR.
        web_bin_cmd="$BOOK_DIR/BookBrowser-linux-64bit"
    else
        web_bin_cmd="$BIN_LOCAL"
    fi

    # 1) .env document with every needed variable.
    run_cmd "mkdir -p '$DATA_DIR'"
    make_env_file "$tmp/app.env"
    copy_to_target "$tmp/app.env" "$DATA_DIR/app.env"
    run_cmd "chmod 600 '$DATA_DIR/app.env'"
    echo "   wrote $DATA_DIR/app.env"

    if [ "$DEPLOY_SERVICE_MODE" = "aws11" ]; then
        # 2a) Runit launcher + static binary name in BOOK_DIR.
        copy_to_target "$BIN_LOCAL" "$web_bin_cmd"
        cat > "$tmp/runit" <<EOF
#!/bin/sh
set -eu
cd "$BOOK_DIR"
if [ -f "$DATA_DIR/app.env" ]; then
    set -a
    . "$DATA_DIR/app.env"
    set +a
fi
nohup ./BookBrowser-linux-64bit -a "$WEB_ADDR" >book.log 2>&1 &
EOF
        copy_to_target "$tmp/runit" "$BOOK_DIR/runit"
        run_cmd "chmod 755 '$BOOK_DIR/runit'"
        echo "   installed $BOOK_DIR/runit"

        # 3a) Restart the web daemon.
        run_cmd "pkill -f BookBrowser-linux-64bit >/dev/null 2>&1 || true"
        run_cmd "cd '$BOOK_DIR' && bash runit"
        echo "   restarted web daemon on $WEB_ADDR"
    else
        # 2b) systemd user unit.
        cat > "$tmp/bookbrowser.service" <<EOF
[Unit]
Description=BookBrowser ebook server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$BOOK_DIR
EnvironmentFile=-$DATA_DIR/app.env
ExecStart=$web_bin_cmd --addr $WEB_ADDR --bookdir $BOOK_DIR
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF

        run_cmd "mkdir -p '$USER_UNIT_DIR'"
        copy_to_target "$tmp/bookbrowser.service" "$USER_UNIT_DIR/bookbrowser.service"
        run_cmd "chmod 644 '$USER_UNIT_DIR/bookbrowser.service'"

        # 3b) Reload + restart the target user's systemd instance.
        run_cmd "export XDG_RUNTIME_DIR=/run/user/\$(id -u); \
            systemctl --user daemon-reload; \
            systemctl --user enable bookbrowser.service >/dev/null 2>&1 || true; \
            systemctl --user restart bookbrowser.service"
        echo "   installed $USER_UNIT_DIR/bookbrowser.service"
    fi

    rm -rf "$tmp"
    echo "   web service deployed"
}

# ---------------------------------------------------------------------------
# Stage 3 - detect and (if missing) deploy the TTS backend
# ---------------------------------------------------------------------------
tts_is_running() {
    run_cmd "curl -fsS -m 3 'http://$TTS_LISTEN:$TTS_PORT/ping' >/dev/null 2>&1 || \
        systemctl --user is-active bookbrowser-tts.service >/dev/null 2>&1" && \
        return 0
    return 1
}

install_tts_unit() {
    local tmp="$1"
    if [ "$DEPLOY_SERVICE_MODE" = "aws11" ]; then
        # System-level unit (needs sudo) as documented for aws11.
        cat > "$tmp/bookbrowser-tts.service" <<EOF
[Unit]
Description=BookBrowser Edge text-to-speech server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$TARGET_USER
Group=$TARGET_USER
WorkingDirectory=$BOOK_DIR
CacheDirectory=bookbrowser-tts
CacheDirectoryMode=0750
Environment=TTS_LISTEN=$TTS_LISTEN
Environment=TTS_PORT=$TTS_PORT
Environment=TTS_CACHE=$TTS_CACHE
Environment=TTS_MAX_CONCURRENCY=$TTS_MAX_CONCURRENCY
Environment=TTS_PARALLEL_CHARS=$TTS_PARALLEL_CHARS
Environment=TTS_PARALLEL_SLOTS=$TTS_PARALLEL_SLOTS
Environment=TTS_SYNTH_TIMEOUT=$TTS_SYNTH_TIMEOUT
Environment=PYTHONDONTWRITEBYTECODE=1
ExecStart=$TTS_VENV/bin/python $BOOK_DIR/tts_server.py
Restart=on-failure
RestartSec=3
TimeoutStopSec=15
MemoryMax=256M
Nice=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
EOF
        copy_to_target "$tmp/bookbrowser-tts.service" /tmp/bookbrowser-tts.service
        run_cmd "sudo mv -f /tmp/bookbrowser-tts.service \
            /etc/systemd/system/bookbrowser-tts.service && \
            sudo systemctl daemon-reload && \
            sudo systemctl enable bookbrowser-tts.service >/dev/null 2>&1; \
            sudo systemctl restart bookbrowser-tts.service"
    else
        # systemd user unit.
        cat > "$tmp/bookbrowser-tts.service" <<EOF
[Unit]
Description=BookBrowser text-to-speech server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$BOOK_DIR
Environment=TTS_LISTEN=$TTS_LISTEN
Environment=TTS_PORT=$TTS_PORT
Environment=TTS_CACHE=$TTS_CACHE
Environment=TTS_MAX_CONCURRENCY=$TTS_MAX_CONCURRENCY
Environment=TTS_PARALLEL_CHARS=$TTS_PARALLEL_CHARS
Environment=TTS_PARALLEL_SLOTS=$TTS_PARALLEL_SLOTS
Environment=TTS_SYNTH_TIMEOUT=$TTS_SYNTH_TIMEOUT
ExecStart=$TTS_VENV/bin/python $BOOK_DIR/tts_server.py
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
        run_cmd "mkdir -p '$USER_UNIT_DIR'"
        copy_to_target "$tmp/bookbrowser-tts.service" "$USER_UNIT_DIR/bookbrowser-tts.service"
        run_cmd "chmod 644 '$USER_UNIT_DIR/bookbrowser-tts.service'"
        run_cmd "export XDG_RUNTIME_DIR=/run/user/\$(id -u); \
            systemctl --user daemon-reload; \
            systemctl --user enable bookbrowser-tts.service >/dev/null 2>&1; \
            systemctl --user restart bookbrowser-tts.service"
    fi
}

deploy_tts() {
    if [ "$DO_FORCE_TTS" = 0 ] && tts_is_running; then
        echo "TTS backend already running on $TTS_LISTEN:$TTS_PORT; skipping."
        return
    fi

    echo "== Deploying TTS backend =="
    local tmp
    tmp="$(mktemp -d)"

    # 1) Python virtualenv.
    if ! run_cmd "test -x '$TTS_VENV/bin/python'"; then
        echo "   creating virtualenv at $TTS_VENV"
        run_cmd "python3 -m venv '$TTS_VENV'" || \
            { echo "ERROR: could not create $TTS_VENV (need python3-venv?)" >&2; exit 1; }
    fi

    # 2) Pinned dependencies.
    run_cmd "mkdir -p '$BOOK_DIR'"
    copy_to_target "$SCRIPT_DIR/deploy/aws11/requirements-tts.txt" "$BOOK_DIR/requirements-tts.txt"
    run_cmd "'$TTS_VENV/bin/pip' install -q -r '$BOOK_DIR/requirements-tts.txt'"

    # 3) Server script.
    copy_to_target "$SCRIPT_DIR/scripts/tts_server.py" "$BOOK_DIR/tts_server.py"
    run_cmd "test -f '$BOOK_DIR/tts_server.py' && chmod 644 '$BOOK_DIR/tts_server.py'"

    # 4) Service unit + start.
    install_tts_unit "$tmp"

    # 5) Wait for it to answer /ping.
    local i
    for i in 1 2 3 4 5 6 7 8 9 10; do
        if run_cmd "curl -fsS -m 2 'http://$TTS_LISTEN:$TTS_PORT/ping' >/dev/null 2>&1"; then
            echo "   TTS backend is up on $TTS_LISTEN:$TTS_PORT"
            rm -rf "$tmp"
            return
        fi
        sleep 1
    done
    rm -rf "$tmp"
    echo "ERROR: TTS backend did not answer /ping after deploy" >&2
    echo "       check: systemctl status bookbrowser-tts.service" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Stage 4 (optional) - ensure the nginx reverse proxy has the /tts/ route
# ---------------------------------------------------------------------------
deploy_nginx() {
    [ "$DEPLOY_NGINX" = 1 ] || return 0
    if [ -z "${DEPLOY_DOMAIN:-}" ]; then
        echo "WARN: DEPLOY_NGINX=1 requires DEPLOY_DOMAIN; skipping nginx." >&2
        return 0
    fi
    echo "== Ensuring nginx reverse proxy for $DEPLOY_DOMAIN =="
    local tmp
    tmp="$(mktemp -d)"
    local site_conf="/tmp/ebook-$DEPLOY_DOMAIN.conf"
    local web_port tts_port
    web_port="${WEB_ADDR##*:}"
    tts_port="$TTS_PORT"

    # Listen port for the web addr (invalid fallback 8092).
    [[ "$web_port" =~ ^[0-9]+$ ]] || web_port=8092

    cat > "$tmp/ebook.conf" <<EOF
server {
    listen 80;
    server_name $DEPLOY_DOMAIN;
    location / { return 301 https://\$host\$request_uri; }
}

server {
    listen 443 ssl;
    server_name $DEPLOY_DOMAIN;

    location /tts/ {
        proxy_pass http://127.0.0.1:$tts_port/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        client_max_body_size 64k;
        proxy_read_timeout 90s;
    }

    location / {
        proxy_pass http://127.0.0.1:$web_port;
    }
}
EOF
    copy_to_target "$tmp/ebook.conf" "$site_conf"
    run_cmd "sudo mv -f '$site_conf' '/etc/nginx/sites-available/ebook-$DEPLOY_DOMAIN.conf' \
        && sudo ln -sf '/etc/nginx/sites-available/ebook-$DEPLOY_DOMAIN.conf' \
           '/etc/nginx/sites-enabled/ebook-$DEPLOY_DOMAIN.conf' \
        && sudo nginx -t \
        && sudo systemctl reload nginx"
    rm -rf "$tmp"
    echo "   nginx site updated for $DEPLOY_DOMAIN (certificates must exist separately)"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
TMP_BASE="$(mktemp -d)"
trap 'rm -rf "$TMP_BASE"' EXIT

echo "Target: ${SSH_HOST:-localhost} (mode: $DEPLOY_SERVICE_MODE)"
build_if_needed
deploy_web
deploy_tts
deploy_nginx

echo
echo "Deploy finished for ${SSH_HOST:-localhost}."