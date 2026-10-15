#!/usr/bin/env bash
# Measure hardware performance counters of a RUNNING VIRTUALBOX VM from the
# HOST machine. VirtualBox guests normally cannot access the PMU themselves,
# so host-side measurement is how you obtain IPC / cache-miss evidence inside
# VirtualBox provisioning scenarios.
#
# Requirements: Linux host with perf (linux-tools). Run as your user (or root
# if perf_event_paranoid blocks you).
#
# Usage:
#   ./host_perf.sh <vm-name-or-pid> <seconds> [output-file]
#
# Output: perf interval (-I 1000) report, one sample per second, matching the
# 1 s metrics.csv intervals. Keep this file next to the run directory; it is
# referenced by README instructions for correlating with plots.
set -euo pipefail

TARGET=${1:?usage: host_perf.sh <vm-name|pid> <seconds> [output-file]}
SECS=${2:-30}
OUT=${3:-host_perf_$(date -u +%Y%m%dT%H%M%S).txt}

if [[ "$TARGET" =~ ^[0-9]+$ ]]; then
  PID=$TARGET
else
  PID=$(pgrep -f "VBoxHeadless.*$TARGET|VBoxHeadless.*--startvm[ =]$TARGET" | head -1 || true)
  [ -n "$PID" ] || PID=$(pgrep -af VBoxHeadless | grep -F "$TARGET" | awk '{print $1}' | head -1 || true)
fi
[ -n "${PID:-}" ] || { echo "error: no VBoxHeadless process found for '$TARGET'" >&2; exit 1; }
echo "monitoring VBoxHeadless pid $PID for ${SECS}s -> $OUT"

# Prefer rich events; fall back to generic ones if the host PMU lacks them.
EVENTS="cycles,instructions,cache-references,cache-misses,LLC-loads,LLC-load-misses,dTLB-load-misses,page-faults,major-faults"
if ! perf list 2>/dev/null | grep -q LLC-load-misses; then
  EVENTS="cycles,instructions,cache-references,cache-misses,page-faults,major-faults"
fi

# perf with -p and no workload runs until interrupted; SIGINT makes it flush
# the interval report and exit.
if timeout --signal=INT "$SECS" \
     perf stat -I 1000 -e "$EVENTS" -o "$OUT" -p "$PID" 2>/dev/null; then
  :
else
  # Some perf versions want an explicit workload after the timeout kill.
  :
fi

if [ ! -s "$OUT" ]; then
  echo "perf failed; retrying with generic events" >&2
  timeout --signal=INT "$SECS" \
    perf stat -I 1000 -e cycles,instructions,cache-references,cache-misses \
    -o "$OUT" -p "$PID" || true
fi
[ -s "$OUT" ] || { echo "error: no counters collected (check perf_event_paranoid / permissions)" >&2; exit 1; }
echo "wrote $OUT"
echo "Interpret: IPC = instructions/cycles; cache-misses/cache-references = miss rate."
echo "Compare seconds with metrics.csv elapsed_s of the same run."
