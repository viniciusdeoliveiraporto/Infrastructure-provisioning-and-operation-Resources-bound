#!/bin/bash

DISK="sda5"
INTERFACE="enp0s3"
INTERVAL=1
SAMPLES=60

sar -u "$INTERVAL" "$SAMPLES" -o cpu.bin >/dev/null 2>&1 &
sar -r "$INTERVAL" "$SAMPLES" -o memory.bin >/dev/null 2>&1 &
sar -d -p "$INTERVAL" "$SAMPLES" -o disk.bin >/dev/null 2>&1 &
sar -n DEV "$INTERVAL" "$SAMPLES" -o network.bin >/dev/null 2>&1 &

wait

# Export data to CSV
sadf -d cpu.bin -- -u > cpu.csv
sadf -d memory.bin -- -r > memory.csv

sadf -d disk.bin -- -d -p |
awk -F',' -v disk="$DISK" 'NR == 1 || index($0, disk) > 0' > disk.csv

sadf -d network.bin -- -n DEV |
awk -F',' -v iface="$INTERFACE" 'NR == 1 || index($0, iface) > 0' > network.csv