# Aplicação CPU-Bound

Aplicação FastAPI para experimentos de provisionamento de recursos e testes de stress (fatoração de números primos).

---

## 1. Execução na VM

### 1.1 Pré-requisitos e Ambiente
A partir da raiz do repositório:
```bash
# 1. Instalar pacotes necessários do sistema (Python, Nginx, Sysstat)
bash scripts/setup-vm.sh

# 2. Criar ambiente virtual e instalar dependências da aplicação
cd apps/cpu-bound
bash scripts/python-env.sh
```

### 1.2 Configurar o Nginx como Proxy Reverso
Edite o arquivo padrão de configuração do Nginx na VM:
```bash
sudo nano /etc/nginx/sites-available/default
```

Dentro do bloco `server { ... }`, localize `location / { ... }` e aponte para a porta interna da aplicação (`8001`):
```nginx
location / {
    proxy_pass http://127.0.0.1:8001;
}
```
Salve (`Ctrl+O`, `Enter`, `Ctrl+X`) e reinicie o Nginx:
```bash
sudo systemctl restart nginx
```

### 1.3 Iniciar a Aplicação
Dentro de `apps/cpu-bound`:
```bash
bash scripts/init-app.sh
```
*(Para executar em background liberando o terminal: `nohup bash scripts/init-app.sh > app.log 2>&1 &`)*.

---

## 2. Testes de Carga (k6 no Host)

A partir da máquina física (host), dentro da raiz do projeto:

```bash
# Demanda 1 (5 VUs — Carga Leve)
k6 run -e VUS=5 -e ITERATIONS=1 load-tests/cpu-bound.js

# Demanda 2 (10 VUs — Carga Média)
k6 run -e VUS=10 -e ITERATIONS=5 load-tests/cpu-bound.js

# Demanda 3 (15 VUs — Carga Pesada)
k6 run -e VUS=15 -e ITERATIONS=10 load-tests/cpu-bound.js
```

---

## 3. Monitoramento na VM (sar)

Durante a execução de cada teste de 60 segundos do k6, execute na VM:
```bash
bash scripts/monitor-sar.sh P1_D1 60
```
Os dados coletados de CPU, memória e I/O serão salvos na pasta `metrics/`.
