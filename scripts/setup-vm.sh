#!/bin/bash

# Atualiza pacotes e instala pré-requisitos na VM Ubuntu
sudo apt update
sudo apt install -y python3 python3-venv python3-pip nginx sysstat git curl htop

# Habilita a coleta de dados do sysstat se necessário
sudo sed -i 's/ENABLED="false"/ENABLED="true"/g' /etc/default/sysstat 2>/dev/null || true
sudo systemctl enable --now sysstat

echo "=== VM setup concluído com sucesso! ==="
