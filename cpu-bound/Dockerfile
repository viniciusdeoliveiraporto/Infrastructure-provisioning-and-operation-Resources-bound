FROM python:3.12-slim

WORKDIR /app

# Instala ferramentas básicas de build e copia arquivos de especificação
COPY pyproject.toml ./

# Instala as dependências da aplicação usando pyproject.toml
RUN pip install --no-cache-dir .

# Copia o código da aplicação
COPY main.py ./

# Expõe a porta padrão do servidor web
EXPOSE 8000

# Executa o Uvicorn
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
