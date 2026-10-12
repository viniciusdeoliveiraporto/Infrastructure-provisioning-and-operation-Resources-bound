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

sadf -d -O separator=, cpu.bin -- -u > cpu.csv 2>/dev/null &
sadf -d -O separator=, memory.bin -- -r > memory.csv 2>/dev/null &
sadf -d -O separator=, disk.bin -- -d -p | grep "$DISK" > disk.csv &
sadf -d -O separator=, network.bin -- -n DEV | grep "$INTERFACE" > network.csv &

wait
