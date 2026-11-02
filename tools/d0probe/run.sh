#!/usr/bin/env bash
# run.sh: D0's Linux checks, run by .github/workflows/d0.yml on a runner with
# GTK4, WebKitGTK 6.0, Xvfb and D-Bus installed. Builds each probe twice, with
# stock Wails and with the GUI libraries loaded lazily (wails-lazy.patch), and
# reports what each costs and whether a window renders.
set -euo pipefail
cd "$(dirname "$0")"
out=${RUNNER_TEMP:-/tmp}/d0
mkdir -p "$out"

build() { # build <variant> : headless and win, -tags purego, no cgo
  CGO_ENABLED=0 go build -tags purego -o "$out/headless-$1" ./headless
  CGO_ENABLED=0 go build -tags purego -o "$out/win-$1" ./win
}

build stock

# The lazy variant: a copy of the same Wails release with the patch applied.
src=$(go list -m -f '{{.Dir}}' github.com/wailsapp/wails/v3)
rm -rf "$out/wails" && cp -R "$src" "$out/wails" && chmod -R u+w "$out/wails"
(cd "$out/wails" && patch -s -p1 < "$OLDPWD/wails-lazy.patch")
go mod edit -replace "github.com/wailsapp/wails/v3=$out/wails"
build lazy
go mod edit -dropreplace github.com/wailsapp/wails/v3

echo "## A process that links Wails and never opens a window"
for v in stock lazy; do
  start=$(date +%s%N)
  for _ in $(seq 30); do "$out/headless-$v" >/dev/null; done
  end=$(date +%s%N)
  echo "$v: $("$out/headless-$v"), run and exit $(( (end - start) / 30000 ))us"
done

# pss <pid> sums the proportional set size of pid and its descendants (the
# WebKit web and network processes), and lists each.
pss() {
  local pids=$1 queue=$1 p kids total=0 kb
  while [ -n "$queue" ]; do
    p=${queue%% *}; queue=${queue#"$p"}; queue=${queue# }
    kids=$(ps -o pid= --ppid "$p" | tr -d ' ' | tr '\n' ' ')
    queue="$queue $kids"; queue=${queue# }; pids="$pids $kids"
  done
  for p in $pids; do
    [ -r "/proc/$p/smaps_rollup" ] || continue
    kb=$(awk '/^Pss:/ {print $2}' "/proc/$p/smaps_rollup")
    total=$((total + kb))
    echo "    $(cat "/proc/$p/comm"): ${kb} kB"
  done
  echo "    total PSS: $((total / 1024)) MB"
}

echo "## One window, rendered"
for v in stock lazy; do
  log="$out/win-$v.log"
  "$out/win-$v" >"$log" 2>&1 &
  pid=$!
  for _ in $(seq 120); do
    grep -q 'rendered after\|timeout\|run:' "$log" && break
    sleep 0.5
  done
  if grep -q 'rendered after' "$log"; then
    echo "$v: $(grep -m1 'rendered after' "$log")"
    sleep 2
    pss "$pid"
  else
    echo "$v: did not render"
    grep -E 'ERROR|CRITICAL|bwrap|SIG' "$log" | head -5 || true
  fi
  wait "$pid" || echo "$v: exited $?"
done
