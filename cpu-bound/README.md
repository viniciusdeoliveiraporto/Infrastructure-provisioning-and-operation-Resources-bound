# Aplicação CPU-Bound — Benchmark de Fatoração de Primos

Aplicação backend desenvolvida em **Python** com **FastAPI**, projetada especificamente para simular uma carga de trabalho com perfil **CPU-Bound** (processamento intensivo) para análise de desempenho, demanda e provisionamento de recursos em máquinas virtuais.

---

## 📌 1. Características do Perfil CPU-Bound

A aplicação executa o algoritmo de **fatoração de inteiros por divisão sucessiva (trial division)**:
- **Computação pura:** Executa repetidos testes de divisibilidade e divisões inteiras em loop até a raiz quadrada do número.
- **Isolamento de recursos:** Não realiza leitura/escrita em disco, não consulta bancos de dados e não faz requisições de rede externas.
- **Memória constante e desprezível:** A estrutura de dados alocada armazena apenas a lista dos fatores primos encontrados, mantendo o consumo de RAM estritamente estável e baixo durante toda a execução.
- **Sensibilidade a provisionamento de CPU:** O tempo de resposta degrada diretamente com a concorrência e a contenção de ciclos de clock de CPU.

---

## 🐳 2. Como Subir a Aplicação com Docker

### 2.1 Construir a imagem Docker
A partir do diretório raiz do projeto ou dentro de `cpu-bound/`:
```bash
docker build -t cpu-bound-app ./cpu-bound
```

### 2.2 Executar o container
```bash
docker run -d --name cpu-bound-container -p 8000:8000 cpu-bound-app
```

> **Dica para testes de provisionamento:** No Docker, você pode restringir recursos da aplicação diretamente caso queira validar localmente:
> ```bash
> docker run -d --name cpu-bound-container --cpus="1.0" -m="512m" -p 8000:8000 cpu-bound-app
> ```

---

## 💻 3. Como Executar Localmente (sem Docker)

1. Certifique-se de ter o Python 3.11+ instalado.
2. Instale o pacote e suas dependências a partir do `pyproject.toml`:
   ```bash
   pip install ./cpu-bound
   ```
3. Inicie o servidor Uvicorn:
   ```bash
   uvicorn cpu-bound.main:app --host 0.0.0.0 --port 8000 --reload
   ```

---

## 🚀 4. Como Usar a Aplicação (Sem Front-end)

Como a aplicação é estritamente **backend**, você pode interagir com ela de três formas:

### Opção A — Interface Interativa no Navegador (Swagger UI) ⭐
O FastAPI gera automaticamente uma interface web completa de testes. Abra no seu navegador:

```text
http://localhost:8000/docs
```
*(Se estiver rodando dentro de uma VM VirtualBox, substitua `localhost` pelo IP da VM, por exemplo `http://192.168.56.101:8000/docs`)*

Na tela do Swagger:
1. Clique no endpoint desejado (ex: `GET /api/factor`).
2. Clique no botão **"Try it out"**.
3. Preencha os parâmetros `number` e `iterations`.
4. Clique em **"Execute"** e veja o resultado e o tempo exato de resposta em JSON.

Alternativamente, a documentação ReDoc está disponível em:
```text
http://localhost:8000/redoc
```

---

### Opção B — Via Linha de Comando (`curl` ou PowerShell)

#### 1. Verificar status da aplicação
```bash
curl -X GET http://localhost:8000/
```
**Resposta esperada:**
```json
{
  "status": "ok",
  "service": "cpu-bound-app",
  "description": "Benchmark de processamento intensivo de CPU (Fatoração de Inteiros)",
  "docs": "/docs"
}
```

#### 2. Health Check
```bash
curl -X GET http://localhost:8000/health
```
**Resposta esperada:**
```json
{"status": "healthy"}
```

#### 3. Consultar métricas de CPU e Memória
```bash
curl -X GET http://localhost:8000/metrics
```
**Resposta esperada:**
```json
{
  "process_cpu_percent": 98.4,
  "system_cpu_percent": 100.0,
  "process_memory_rss_mb": 42.15,
  "system_memory_percent": 24.3,
  "cpu_count_logical": 2,
  "cpu_count_physical": 2
}
```

#### 4. Executar carga de processamento CPU-Bound
```bash
# Carga padrão (fatoração de 999999999999)
curl -X GET "http://localhost:8000/api/factor?number=999999999999&iterations=1"

# Carga aumentada (fatoração de número primo grande com 10 repetições)
curl -X GET "http://localhost:8000/api/factor?number=100000000003&iterations=10"
```
**Resposta esperada:**
```json
{
  "number": 999999999999,
  "iterations": 1,
  "prime_factors": [3, 3, 3, 7, 11, 13, 37, 101, 9901],
  "execution_time_seconds": 0.082145
}
```

---

### Opção C — Acesso Direto pelo Navegador
Você pode simplesmente colar a URL na barra de endereços do seu navegador:
- `http://localhost:8000/api/factor?number=999999999999`
- `http://localhost:8000/metrics`

---

## ⚙️ 5. Parâmetros da Rota CPU-Bound (`/api/factor`)

| Parâmetro | Tipo | Padrão | Descrição |
|-----------|------|--------|-----------|
| `number` | Inteiro | `999999999999` | Número a ser decomposto. Números primos ou com fatores primos grandes forçam mais iterações do laço. |
| `iterations` | Inteiro | `1` | Quantidade de repetições sequenciais da decomposição. Ideal para calibrar a duração do processamento para testes de stress. |

---

## 📊 6. Monitoramento nos Experimentos

Durante os testes de stress (gerados a partir do host externo):
1. **Métricas da Aplicação:** A rota `/metrics` expõe `process_cpu_percent` e memória RSS.
2. **Métricas do Host/VM:** Acompanhar na VM com ferramentas como:
   ```bash
   htop
   # ou
   vmstat 1
   ```
3. **Métricas de Carga:** Coletadas pela ferramenta externa de testes (ex: k6), registrando latência (p50, p90, p95, p99), vazão (RPS) e taxa de erros.
