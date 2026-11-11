#!/bin/bash

# Inicia a aplicação CPU-bound com Uvicorn escutando em localhost na porta 8001
source .venv/bin/activate
uvicorn main:app --host 127.0.0.1 --port 8001
