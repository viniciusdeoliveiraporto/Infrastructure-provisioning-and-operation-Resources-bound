#!/usr/bin/env bash
# Local runner for the memory-bound experiment plan (E1/E2/E3).
# Usage: scripts/run_memory_bound_experiments.sh [server_bin] [loadgen_bin]
# Env: ADDR, DURATION, WARMUP, REPEATS_E1, REPEATS_E2E3, SKIP_PLOTS
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MEMBOUND="$(cd "$SCRIPT_DIR/.." && pwd)"          # .../load-tests/memory-bound
SERVER_BIN="${1:-/tmp/opencode/kvbin/server}"
LOADGEN_BIN="${2:-/tmp/opencode/kvbin/loadgen}"
ADDR="${ADDR:-127.0.0.1:8097}"
URL="http://$ADDR"
DURATION="${DURATION:-120s}"
WARMUP="${WARMUP:-10s}"
REPEATS_E1="${REPEATS_E1:-3}"
REPEATS_E2E3="${REPEATS_E2E3:-2}"
SKIP_PLOTS="${SKIP_PLOTS:-0}"
RESULTS="$MEMBOUND/results"
PLOT="$MEMBOUND/scripts/plot_results.py"
PY="${PLOT_PY:-python3}"

[[ -x "$SERVER_BIN" ]]  || { echo "missing server bin: $SERVER_BIN" >&2; exit 1; }
[[ -x "$LOADGEN_BIN" ]] || { echo "missing loadgen bin: $LOADGEN_BIN" >&2; exit 1; }

AVAIL_KB=$(awk '/MemAvailable/{print $2}' /proc/meminfo)
AVAIL_MIB=$(( AVAIL_KB / 1024 ))
BUDGET_MIB=$(( AVAIL_MIB * 80 / 100 ))
MAX_MIB=$(( BUDGET_MIB - 1536 ))   # headroom for OS + client + page cache
(( MAX_MIB < 64 )) && { echo "not enough free RAM" >&2; exit 1; }

# E1: small (LLC-resident) vs large (DRAM-resident)
E1_SMALL_ENTRIES=2048
E1_LARGE_MIB=512
(( E1_LARGE_MIB > MAX_MIB )) && E1_LARGE_MIB=$MAX_MIB
E1_LARGE_ENTRIES=$(( E1_LARGE_MIB * 1024 ))

# E2 ladder (MiB at 1 KiB values)
E2_SIZES=(10 100 512)
(( MAX_MIB >= 1024 )) && E2_SIZES+=(1024)
FITTED=()
for m in "${E2_SIZES[@]}"; do
  (( m <= MAX_MIB )) && FITTED+=("$m")
done
E2_SIZES=("${FITTED[@]}")

E3_ENTRIES=$E1_LARGE_ENTRIES
E3_MIB=$E1_LARGE_MIB

echo "== environment =="
echo "MemAvailable=${AVAIL_MIB} MiB budget80=${BUDGET_MIB} MiB max=${MAX_MIB} MiB"
echo "E1: small=${E1_SMALL_ENTRIES} entries (~2 MiB) vs large=${E1_LARGE_MIB} MiB"
echo "E2 sizes: ${E2_SIZES[*]} MiB; E3 dataset: ${E3_MIB} MiB"
echo "server=$SERVER_BIN loadgen=$LOADGEN_BIN addr=$ADDR duration=$DURATION"

SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

start_server() {
  cleanup
  SERVER_PID=""
  "$SERVER_BIN" -addr "$ADDR" -admin -mem-limit-pct 80 "$@" \
    >/tmp/opencode/kv-experiment-server.log 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 80); do
    curl -sf "$URL/health" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  echo "server not healthy; log:" >&2
  tail -30 /tmp/opencode/kv-experiment-server.log >&2
  exit 1
}

run_loadgen() { # label mode conc entries [extra...]
  local label=$1 mode=$2 conc=$3 entries=$4; shift 4
  echo "-- run $label (mode=$mode c=$conc entries=$entries)"
  PATH="${PLOT_PATH:-/tmp/opencode/kvplots/bin}:$PATH" \
  "$LOADGEN_BIN" -url "$URL" -mode "$mode" \
    -dataset-entries "$entries" -value-size 1024 \
    -concurrency "$conc" -duration "$DURATION" -warmup "$WARMUP" \
    -seed 42 -label "$label" -results-dir "$RESULTS" \
    -plot-script "$([[ $SKIP_PLOTS == 1 ]] && echo '' || echo "$PLOT")" \
    "$@"
}

echo
echo "== E1: cache-resident (~2 MiB) vs DRAM-resident (${E1_LARGE_MIB} MiB) =="
start_server -populate-entries "$E1_SMALL_ENTRIES" -populate-value-size 1024
for i in $(seq 1 "$REPEATS_E1"); do
  run_loadgen "e1-small-r$i" random-read 8 "$E1_SMALL_ENTRIES" -no-populate
done
start_server -populate-entries "$E1_LARGE_ENTRIES" -populate-value-size 1024
for i in $(seq 1 "$REPEATS_E1"); do
  run_loadgen "e1-large-r$i" random-read 8 "$E1_LARGE_ENTRIES" -no-populate
done

echo
echo "== E2: dataset size sweep (random-read, c8) =="
for mib in "${E2_SIZES[@]}"; do
  entries=$(( mib * 1024 ))
  tag="${mib}mib"
  (( mib >= 1024 )) && tag="1gib"
  start_server -populate-entries "$entries" -populate-value-size 1024
  for i in $(seq 1 "$REPEATS_E2E3"); do
    run_loadgen "e2-${tag}-r$i" random-read 8 "$entries" -no-populate
  done
done

echo
echo "== E3: concurrency wall (random-read, ${E3_MIB} MiB) =="
start_server -populate-entries "$E3_ENTRIES" -populate-value-size 1024
for c in 1 4 8 16 32; do
  for i in $(seq 1 "$REPEATS_E2E3"); do
    run_loadgen "e3-c$c-r$i" random-read "$c" "$E3_ENTRIES" -no-populate
  done
done

if [[ $SKIP_PLOTS != 1 ]]; then
  echo
  echo "== compare plots =="
  # loadgen writes results into timestamped dirs containing the label
  find_run() { ls -d "$RESULTS"/*-"$1" 2>/dev/null | sort | tail -1; }

  e1s=$(find_run "e1-small-r$REPEATS_E1")
  e1l=$(find_run "e1-large-r$REPEATS_E1")
  [[ -n "$e1s" && -n "$e1l" ]] && \
    "$PY" "$PLOT" compare "$e1s" "$e1l" \
      -o "$RESULTS/e1-compare" --labels "small (~2 MiB LLC),large (${E1_LARGE_MIB} MiB DRAM)" || true

  e2_runs=(); e2_labels=()
  for mib in "${E2_SIZES[@]}"; do
    tag="${mib}mib"; (( mib >= 1024 )) && tag="1gib"
    d=$(find_run "e2-${tag}-r$REPEATS_E2E3")
    [[ -n "$d" ]] && e2_runs+=("$d") && e2_labels+=("${mib} MiB")
  done
  if (( ${#e2_runs[@]} >= 2 )); then
    IFS=,; lab="${e2_labels[*]}"; unset IFS
    "$PY" "$PLOT" compare "${e2_runs[@]}" -o "$RESULTS/e2-compare" --labels "$lab" || true
  fi

  e3_runs=(); e3_labels=()
  for c in 1 4 8 16 32; do
    d=$(find_run "e3-c$c-r$REPEATS_E2E3")
    [[ -n "$d" ]] && e3_runs+=("$d") && e3_labels+=("c$c")
  done
  if (( ${#e3_runs[@]} >= 2 )); then
    IFS=,; lab="${e3_labels[*]}"; unset IFS
    "$PY" "$PLOT" compare "${e3_runs[@]}" -o "$RESULTS/e3-compare" --labels "$lab" || true
  fi
fi

echo
echo "== done =="
echo "Results under $RESULTS (e1-*, e2-*, e3-*, *-compare/)"
