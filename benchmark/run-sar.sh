#!/bin/bash

DISK="sda5"
INTERFACE="enp0s3"
INTERVAL=1
SAMPLES=60

sar -u "$INTERVAL" "$SAMPLES" -o cpu.bin >/dev/null 2>&1 &
PID_CPU=$!

sar -r "$INTERVAL" "$SAMPLES" -o memory.bin >/dev/null 2>&1 &
PID_MEMORY=$!

sar -d -p "$INTERVAL" "$SAMPLES" -o disk.bin >/dev/null 2>&1 &
PID_DISK=$!

sar -n DEV "$INTERVAL" "$SAMPLES" -o network.bin >/dev/null 2>&1 &
PID_NETWORK=$!

wait "$PID_CPU"
wait "$PID_MEMORY"
wait "$PID_DISK"
wait "$PID_NETWORK"

sadf -d -O separator=, cpu.bin -- -u > cpu.csv 2>/dev/null
sadf -d -O separator=, memory.bin -- -r > memory.csv 2>/dev/null

sadf -d -O separator=, disk.bin -- -d -p |
awk -F';' -v disk="$DISK" 'NR == 1 || index($0, disk) > 0' > disk.csv

sadf -d -O separator=, network.bin -- -n DEV |
awk -F';' -v iface="$INTERFACE" 'NR == 1 || index($0, iface) > 0' > network.csv
