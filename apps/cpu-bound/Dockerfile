# Imagem base oficial do Python slim (leve e segura)
FROM python:3.12-slim

# Evita que o Python gere arquivos .pyc e força flush imediato de stdout/stderr
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

# Instala dependências do sistema necessárias para compilação eventual do psutil
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    python3-dev \
    && rm -rf /var/lib/apt/lists/*

# Copia e instala as dependências Python
COPY requirements.txt .
RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir -r requirements.txt

# Copia o código da aplicação
COPY main.py .

# Cria diretório para logs
RUN mkdir -p /app/logs

# Expõe a porta 8001
EXPOSE 8001

# Executa o Uvicorn escutando em todas as interfaces
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8001"]
