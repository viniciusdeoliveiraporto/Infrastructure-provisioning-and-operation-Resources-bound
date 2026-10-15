import os
import time
import psutil
from fastapi import FastAPI, Query

app = FastAPI(
    title="CPU-Bound Benchmark API",
    description="Aplicação backend para experimentos de provisionamento e carga computacional focada em processamento intensivo de CPU.",
    version="0.1.0"
)

# Inicializa o monitoramento de CPU do processo
process = psutil.Process(os.getpid())
process.cpu_percent(interval=None)


def prime_factors(n: int) -> list[int]:
    """Encontra os fatores primos de n usando divisão sucessiva (trial division)."""
    factors = []
    d = 2
    temp = n
    while d * d <= temp:
        while temp % d == 0:
            factors.append(d)
            temp //= d
        d += 1
    if temp > 1:
        factors.append(temp)
    return factors


@app.get("/")
def home():
    """Rota raiz com informações básicas da aplicação."""
    return {
        "status": "ok",
        "service": "cpu-bound-app",
        "description": "Benchmark de processamento intensivo de CPU (Fatoração de Inteiros)",
        "docs": "/docs"
    }


@app.get("/health")
def health():
    """Verificação de integridade (health check) do serviço."""
    return {"status": "healthy"}


@app.get("/metrics")
def metrics():
    """Métricas em tempo real de consumo de recursos do processo e do sistema."""
    mem_info = process.memory_info()
    return {
        "process_cpu_percent": process.cpu_percent(interval=None),
        "system_cpu_percent": psutil.cpu_percent(interval=None),
        "process_memory_rss_mb": round(mem_info.rss / (1024 * 1024), 2),
        "system_memory_percent": psutil.virtual_memory().percent,
        "cpu_count_logical": psutil.cpu_count(logical=True),
        "cpu_count_physical": psutil.cpu_count(logical=False),
    }


@app.get("/api/factor")
def factor_number(
    number: int = Query(
        999999999999,
        description="Número inteiro a ser decomposto em fatores primos"
    ),
    iterations: int = Query(
        1,
        ge=1,
        le=1000,
        description="Quantidade de repetições da fatoração para modular a carga de CPU"
    )
):
    """
    Executa a decomposição em fatores primos.
    A carga de CPU pode ser ajustada variando o número ou a quantidade de iterações.
    """
    start_time = time.perf_counter()

    factors = []
    for _ in range(iterations):
        factors = prime_factors(number)

    elapsed_time = time.perf_counter() - start_time

    return {
        "number": number,
        "iterations": iterations,
        "prime_factors": factors,
        "execution_time_seconds": round(elapsed_time, 6)
    }
