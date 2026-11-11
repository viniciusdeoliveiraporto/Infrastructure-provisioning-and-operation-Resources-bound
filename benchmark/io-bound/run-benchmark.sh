
#!/bin/bash

set -euo pipefail

BASE_URL="http://localhost:8080"
DURATION="60s"

CSV_FILE="dados2_10MB.csv"
VUS=10

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

RESULT_DIR="$SCRIPT_DIR/results"
mkdir -p "$RESULT_DIR"

if [[ ! -f "$SCRIPT_DIR/csvs/$CSV_FILE" ]]; then
    echo "Erro: CSV doesn't find: csvs/$CSV_FILE"
    exit 1
fi

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
CSV_NAME="${CSV_FILE%.csv}"

RESULT_FILE="$RESULT_DIR/resultado_${CSV_NAME}_${VUS}vus_${TIMESTAMP}.json"

echo "Iniciando benchmark..."
echo "URL:       $BASE_URL"
echo "CSV:       $CSV_FILE"
echo "Usuários:  $VUS"
echo "Duração:   $DURATION"
echo "Resultado: $RESULT_FILE"
echo

k6 run \
    -e "CSV_FILE=$CSV_FILE" \
    -e "VUS=$VUS" \
    -e "DURATION=$DURATION" \
    -e "BASE_URL=$BASE_URL" \
    --summary-export "$RESULT_FILE" \
    "$SCRIPT_DIR/upload-csv.js"

echo
echo "Resultado salvo em: $RESULT_FILE"
