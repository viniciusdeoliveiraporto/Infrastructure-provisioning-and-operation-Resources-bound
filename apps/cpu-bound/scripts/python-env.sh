#!/bin/bash

# Configura o ambiente virtual Python e instala as dependências
python3 -m venv .venv
source .venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt
