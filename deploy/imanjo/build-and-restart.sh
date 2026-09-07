#!/usr/bin/env bash
#
# Build + swap-in the new backend binary, then restart the service.
#
#   RUN IT AS A SCRIPT, NOT BY PASTING INTO YOUR LOGIN SHELL:
#       bash deploy/imanjo/build-and-restart.sh
#
# Pasting a block that contains `exit 1` runs that `exit` in your interactive
# shell, which closes the SSH session the moment `go build` fails — that is the
# "the window closes" symptom. As a script, `exit` only ends the script.
#
# The build step is memory-hungry. On a small VPS the kernel OOM-killer kills the
# compiler (and can take the shell with it). This script:
#   * caps compiler parallelism (GOFLAGS=-p, GOMAXPROCS) to lower peak RAM,
#   * adds a temporary 2G swapfile for the build if the box is low on memory,
#   * prints exactly where it stopped instead of vanishing.

set -Eeuo pipefail

APP_DIR="${IMANJO_APP_DIR:-/var/www/vhosts/imanjo.com/api.imanjo.com}"
SERVICE="imanjo-api.service"
PORT="${IMANJO_PORT:-8187}"
BIN="bin/imanjo-api"
SWAPFILE="/var/tmp/imanjo-build.swap"
ADDED_SWAP=0

log()  { printf '\n=== %s ===\n' "$*"; }
fail() { printf '\n!!! FAILED at: %s\n' "$*" >&2; }
trap 'fail "line $LINENO (exit $?)"' ERR

cleanup() {
  if [ "$ADDED_SWAP" = "1" ]; then
    swapoff "$SWAPFILE" 2>/dev/null || true
    rm -f "$SWAPFILE"
    echo "(removed temporary build swapfile)"
  fi
}
trap cleanup EXIT

cd "$APP_DIR"

export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
hash -r

# ---- memory guard -----------------------------------------------------------
log "MEMORY BEFORE BUILD"
free -h || true
MEM_AVAIL_MB="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
SWAP_TOTAL_KB="$(awk '/SwapTotal/ {print $2}' /proc/meminfo)"
echo "available RAM: ${MEM_AVAIL_MB} MB, swap: $((SWAP_TOTAL_KB/1024)) MB"

if [ "${MEM_AVAIL_MB:-0}" -lt 1500 ] && [ "${SWAP_TOTAL_KB:-0}" -lt 1048576 ]; then
  log "ADDING TEMPORARY 2G SWAP FOR THE BUILD"
  rm -f "$SWAPFILE"
  if fallocate -l 2G "$SWAPFILE" 2>/dev/null || dd if=/dev/zero of="$SWAPFILE" bs=1M count=2048 status=none; then
    chmod 600 "$SWAPFILE"
    mkswap "$SWAPFILE" >/dev/null
    swapon "$SWAPFILE"
    ADDED_SWAP=1
    free -h || true
  else
    echo "could not create swapfile — continuing, build may still be OOM-killed" >&2
  fi
fi

# ---- build ----------------------------------------------------------------
log "BUILD NEW BACKEND"
rm -f "${BIN}.new"

# -p limits parallel compile/link actions; GOMAXPROCS caps goroutines inside each.
# CGO off keeps it single-toolchain and lighter. GOGC lower = collect sooner.
if ! CGO_ENABLED=0 GOFLAGS="-p=2" GOMAXPROCS=2 GOGC=50 \
     go build -trimpath -o "${BIN}.new" ./cmd/server; then
  status=$?
  fail "go build (exit $status)"
  if [ "$status" -eq 137 ] || dmesg 2>/dev/null | tail -n 40 | grep -qi 'killed process.*go\|Out of memory'; then
    echo "-> looks OOM-killed. Add permanent swap or build off-box (see notes at the bottom)." >&2
  fi
  echo "CURRENT PRODUCTION UNCHANGED"
  exit 1
fi

chown --reference="$BIN" "${BIN}.new"
chmod --reference="$BIN" "${BIN}.new"
echo
ls -lh "$BIN" "${BIN}.new"

# ---- swap in ------------------------------------------------------------------
log "BACKUP CURRENT BINARY"
TS="$(date +%Y%m%d_%H%M%S)"
cp -a "$BIN" "${BIN}.backup-${TS}"
# keep only the 5 newest backups
ls -1t "${BIN}".backup-* 2>/dev/null | tail -n +6 | xargs -r rm -f

log "INSTALL NEW BINARY"
mv -f "${BIN}.new" "$BIN"

log "RESTART SERVICE"
systemctl restart "$SERVICE"
sleep 3

log "VERIFY SERVICE"
if ! systemctl is-active --quiet "$SERVICE"; then
  fail "service not active after restart — rolling back"
  mv -f "${BIN}.backup-${TS}" "$BIN"
  systemctl restart "$SERVICE"
  journalctl -u "$SERVICE" --since "-2 minutes" --no-pager -o short-iso | tail -60
  exit 1
fi

PID="$(systemctl show -p MainPID --value "$SERVICE")"
echo "active. PID=$PID"
readlink -f "/proc/$PID/exe" || true
echo
ss -lntp | grep ":${PORT}" || echo "(nothing listening on :${PORT} yet — check logs)"

log "LAST LOGS"
journalctl -u "$SERVICE" --since "-2 minutes" --no-pager -o short-iso | tail -80

log "DONE"

# ----------------------------------------------------------------------------
# If the build keeps getting OOM-killed, pick one:
#
# 1) Permanent swap (one time, fixes it for good on a 1-2G box):
#      sudo fallocate -l 4G /swapfile && sudo chmod 600 /swapfile
#      sudo mkswap /swapfile && sudo swapon /swapfile
#      echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
#
# 2) Build off-box and copy just the binary up (no compiler on the server):
#      # on your machine, inside back/:
#      GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o imanjo-api.new ./cmd/server
#      scp imanjo-api.new root@SERVER:$APP_DIR/bin/
#      # then on the server run only the "BACKUP / INSTALL / RESTART" steps above.
#
# 3) Run this script under tmux so an SSH drop can't kill the build:
#      tmux new -s deploy 'bash deploy/imanjo/build-and-restart.sh; bash'
