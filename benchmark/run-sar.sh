#!/bin/bash

DISK="sda5"
INTERFACE="enp0s3"
INTERVAL=1
SAMPLES=60

sar -u "$INTERVAL" "$SAMPLES" -o cpu.bin
sadf -d -O separator=, cpu.bin -- -u > cpu.csv

sar -r "$INTERVAL" "$SAMPLES" -o memory.bin
sadf -d -O separator=, memory.bin -- -r > memory.csv

sar -d -p "$INTERVAL" "$SAMPLES" -o disk.bin
sadf -d -O separator=, disk.bin -- -d -p | grep "$DISK" > disk.csv

sar -n DEV "$INTERVAL" "$SAMPLES" -o network.bin
sadf -d -O separator=, network.bin -- -n DEV | grep "$INTERFACE" > network.csv
