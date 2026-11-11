#!/bin/bash

# Uso: bash scripts/monitor-sar.sh <P1_D1|P1_D2|...> [duracao_segundos]
# Exemplo: bash scripts/monitor-sar.sh P1_D1 60

TEST_ID=${1:-"teste"}
DURATION=${2:-60}
OUT_DIR="metrics"

mkdir -p "$OUT_DIR"

echo "=== Iniciando monitoramento SAR para [$TEST_ID] por $DURATION segundos ==="

# CPU (%usr, %nice, %sys, %iowait, %steal, %idle)
sar -u 1 "$DURATION" > "${OUT_DIR}/sar_cpu_${TEST_ID}.txt" &
PID_CPU=$!

# Memória (kbmemfree, kbavail, kbmemused, %memused, etc)
sar -r 1 "$DURATION" > "${OUT_DIR}/sar_mem_${TEST_ID}.txt" &
PID_MEM=$!

# I/O e Transferência de disco (tps, rtps, wtps, bread/s, bwrtn/s)
sar -b 1 "$DURATION" > "${OUT_DIR}/sar_io_${TEST_ID}.txt" &
PID_IO=$!

echo "SAR em execução (PIDs: CPU=$PID_CPU, MEM=$PID_MEM, IO=$PID_IO)..."
echo "Aguardando término da coleta..."

wait $PID_CPU $PID_MEM $PID_IO

echo "=== Coleta concluída! Arquivos salvos em ${OUT_DIR}/ ==="
ls -lh "${OUT_DIR}/"*${TEST_ID}*
