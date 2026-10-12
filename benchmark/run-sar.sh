#!/bin/bash

DISK="sda5"
INTERFACE="enp0s3"
INTERVAL=1
SAMPLES=60

sar -u "$INTERVAL" "$SAMPLES" -o cpu.bin &
sar -r "$INTERVAL" "$SAMPLES" -o memory.bin &
sar -d -p "$INTERVAL" "$SAMPLES" -o disk.bin &
sar -n DEV "$INTERVAL" "$SAMPLES" -o network.bin &

wait

sadf -d -O separator=, cpu.bin -- -u > cpu.csv &
sadf -d -O separator=, memory.bin -- -r > memory.csv &
sadf -d -O separator=, disk.bin -- -d -p | grep "$DISK" > disk.csv &
sadf -d -O separator=, network.bin -- -n DEV | grep "$INTERFACE" > network.csv &

wait
