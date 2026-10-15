import os
import glob
import json
import re
import matplotlib.pyplot as plt

# Diretórios padrão
METRICS_DIR = "metrics"
RESULTS_DIR = "results_plots"

os.makedirs(RESULTS_DIR, exist_ok=True)


def parse_sar_cpu(filepath):
    """Extrai %usr, %sys e %idle de um arquivo gerado pelo sar -u."""
    times = []
    usr = []
    sys = []
    idle = []
    
    with open(filepath, 'r', encoding='utf-8', errors='ignore') as f:
        for line in f:
            parts = line.split()
            # Identifica linhas com dados de CPU (ex: HH:MM:SS all %usr %nice %sys ...)
            if len(parts) >= 8 and parts[1] == 'all' and parts[0] != 'Média:' and parts[0] != 'Average:':
                try:
                    t = parts[0]
                    u = float(parts[2].replace(',', '.'))
                    s = float(parts[4].replace(',', '.'))
                    idl = float(parts[-1].replace(',', '.'))
                    times.append(t)
                    usr.append(u + s) # %user + %system
                    idle.append(idl)
                except ValueError:
                    continue
    return times, usr, idle


def plot_cpu_metrics(test_id):
    """Gera gráficos de CPU para um teste específico."""
    cpu_file = os.path.join(METRICS_DIR, f"sar_cpu_{test_id}.txt")
    if not os.path.exists(cpu_file):
        print(f"Arquivo não encontrado: {cpu_file}")
        return

    times, cpu_used, cpu_idle = parse_sar_cpu(cpu_file)
    if not times:
        print(f"Sem dados válidos de CPU em: {cpu_file}")
        return

    # Gráfico de Utilização de CPU
    plt.figure(figsize=(10, 5))
    plt.plot(range(len(cpu_used)), cpu_used, label='% CPU (user + system)', color='royalblue', linewidth=2)
    plt.title(f"Utilização de CPU — {test_id}", fontsize=14)
    plt.xlabel("Tempo (segundos)", fontsize=12)
    plt.ylabel("% Utilização", fontsize=12)
    plt.ylim(0, 105)
    plt.grid(True, linestyle='--', alpha=0.6)
    plt.legend()
    plt.tight_layout()
    plt.savefig(os.path.join(RESULTS_DIR, f"cpu_utilizacao_{test_id}.png"))
    plt.close()

    # Gráfico de CPU Ociosa
    plt.figure(figsize=(10, 5))
    plt.plot(range(len(cpu_idle)), cpu_idle, label='% CPU Ociosa (idle)', color='forestgreen', linewidth=2)
    plt.title(f"CPU Ociosa — {test_id}", fontsize=14)
    plt.xlabel("Tempo (segundos)", fontsize=12)
    plt.ylabel("% Ocioso", fontsize=12)
    plt.ylim(0, 105)
    plt.grid(True, linestyle='--', alpha=0.6)
    plt.legend()
    plt.tight_layout()
    plt.savefig(os.path.join(RESULTS_DIR, f"cpu_idle_{test_id}.png"))
    plt.close()

    print(f"[OK] Gráficos de CPU salvos para {test_id} em {RESULTS_DIR}/")


if __name__ == "__main__":
    print("=== Gerador de Gráficos de Desempenho ===")
    cpu_files = glob.glob(os.path.join(METRICS_DIR, "sar_cpu_*.txt"))
    if not cpu_files:
        print(f"Nenhum arquivo encontrado em {METRICS_DIR}/. Execute os testes primeiro!")
    for cf in cpu_files:
        test_id = os.path.basename(cf).replace("sar_cpu_", "").replace(".txt", "")
        plot_cpu_metrics(test_id)
