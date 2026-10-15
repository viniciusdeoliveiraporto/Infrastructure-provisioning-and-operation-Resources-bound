import json
import os
import matplotlib.pyplot as plt

OUTPUT_DIR = "graficos_relatorio"
os.makedirs(OUTPUT_DIR, exist_ok=True)

# Cores e estilos por provisionamento
styles = {
    'P1 (1 vCPU, 1 GB)': {'color': '#e63946', 'marker': 'o', 'linestyle': '--', 'key': 'P1'},
    'P2 (2 vCPUs, 1 GB)': {'color': '#1d3557', 'marker': 's', 'linestyle': '-', 'key': 'P2'},
    'P3 (1 vCPU, 2 GB)': {'color': '#e76f51', 'marker': '^', 'linestyle': '--', 'key': 'P3'},
    'P4 (2 vCPUs, 2 GB)': {'color': '#2a9d8f', 'marker': 'D', 'linestyle': '-', 'key': 'P4'},
}

demandas = ['Demanda 1\n(5 VUs)', 'Demanda 2\n(10 VUs)', 'Demanda 3\n(15 VUs)']
d_keys = ['D1', 'D2', 'D3']

# Carrega os dados dos 12 testes
data = {}
for p in ['P1', 'P2', 'P3', 'P4']:
    data[p] = {}
    for d in d_keys:
        fname = f'k6_{p}_{d}.json'
        with open(fname, 'r', encoding='utf-8') as f:
            content = json.load(f)
            m = content['metrics']
            data[p][d] = {
                'reqs': m['http_reqs']['count'],
                'avg': round(m['http_req_duration']['avg'], 2),
                'p50': round(m['http_req_duration']['med'], 2),
                'p95': round(m['http_req_duration']['p(95)'], 2)
            }

# ==============================================================
# 1. LINHAS AGRUPADAS: Quantidade de Requisições (Throughput)
# ==============================================================
plt.figure(figsize=(9, 5.5))
for label, cfg in styles.items():
    y = [data[cfg['key']][d]['reqs'] for d in d_keys]
    plt.plot(demandas, y, label=label, color=cfg['color'], marker=cfg['marker'],
             markersize=8, linewidth=2.5, linestyle=cfg['linestyle'])
    for i, txt in enumerate(y):
        plt.annotate(f"{txt:,}".replace(",", "."), (demandas[i], y[i]),
                     textcoords="offset points", xytext=(0, 8), ha='center', fontsize=9, fontweight='semibold')

plt.title('Throughput: Quantidade de Requisições por Demanda', fontsize=13, fontweight='bold', pad=15)
plt.xlabel('Perfil de Demanda', fontsize=11, labelpad=10)
plt.ylabel('Requisições Concluídas em 60s', fontsize=11)
plt.grid(True, linestyle=':', alpha=0.6)
plt.legend(title='Provisionamento', fontsize=10, title_fontsize=10.5)
plt.ylim(0, 160000)
plt.tight_layout()
plt.savefig(os.path.join(OUTPUT_DIR, 'linhas_quantidade_requisicoes.png'), dpi=300)
plt.close()

# ==============================================================
# 2. LINHAS AGRUPADAS: Latência Média (ms)
# ==============================================================
plt.figure(figsize=(9, 5.5))
for label, cfg in styles.items():
    y = [data[cfg['key']][d]['avg'] for d in d_keys]
    plt.plot(demandas, y, label=label, color=cfg['color'], marker=cfg['marker'],
             markersize=8, linewidth=2.5, linestyle=cfg['linestyle'])
    for i, txt in enumerate(y):
        plt.annotate(f"{txt:.1f} ms", (demandas[i], y[i]),
                     textcoords="offset points", xytext=(0, 8), ha='center', fontsize=9, fontweight='semibold')

plt.title('Latência Média por Demanda', fontsize=13, fontweight='bold', pad=15)
plt.xlabel('Perfil de Demanda', fontsize=11, labelpad=10)
plt.ylabel('Tempo Médio de Resposta (ms)', fontsize=11)
plt.grid(True, linestyle=':', alpha=0.6)
plt.legend(title='Provisionamento', fontsize=10, title_fontsize=10.5)
plt.tight_layout()
plt.savefig(os.path.join(OUTPUT_DIR, 'linhas_latencia_media.png'), dpi=300)
plt.close()

# ==============================================================
# 3. LINHAS AGRUPADAS: Latência P95 (ms)
# ==============================================================
plt.figure(figsize=(9, 5.5))
for label, cfg in styles.items():
    y = [data[cfg['key']][d]['p95'] for d in d_keys]
    plt.plot(demandas, y, label=label, color=cfg['color'], marker=cfg['marker'],
             markersize=8, linewidth=2.5, linestyle=cfg['linestyle'])
    for i, txt in enumerate(y):
        plt.annotate(f"{txt:.1f} ms", (demandas[i], y[i]),
                     textcoords="offset points", xytext=(0, 8), ha='center', fontsize=9, fontweight='semibold')

plt.title('Latência Percentil 95 (P95) por Demanda', fontsize=13, fontweight='bold', pad=15)
plt.xlabel('Perfil de Demanda', fontsize=11, labelpad=10)
plt.ylabel('Tempo de Resposta P95 (ms)', fontsize=11)
plt.grid(True, linestyle=':', alpha=0.6)
plt.legend(title='Provisionamento', fontsize=10, title_fontsize=10.5)
plt.tight_layout()
plt.savefig(os.path.join(OUTPUT_DIR, 'linhas_latencia_p95.png'), dpi=300)
plt.close()

print("[OK] Gráficos de linhas agrupadas gerados com sucesso!")
