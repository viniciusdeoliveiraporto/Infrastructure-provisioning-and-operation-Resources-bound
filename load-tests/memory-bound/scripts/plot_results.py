#!/usr/bin/env python3
"""Generate performance plots from loadgen benchmark results.

Single run:
    python3 scripts/plot_results.py results/<run-id>

Comparison of multiple runs (plots vs elapsed time; missing metrics are
skipped with a message, never abort the process):
    python3 scripts/plot_results.py compare results/runA results/runB [-o OUTDIR] [--labels a,b]

Headless-safe: the Agg backend is forced, so this works over SSH on EC2
without a graphical desktop. Raw data (metrics.csv / summary.json) is never
modified. Exit code is 0 when at least the run directories were readable;
plot-level failures print an error and continue with the remaining plots.
"""

import argparse
import csv
import json
import math
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402

REQUIRED_COLUMNS = {"elapsed_s", "p50_ms", "p95_ms", "p99_ms", "rps", "payload_mib_s"}


def read_metrics(run_dir):
    """Read metrics.csv into {column: [float|None, ...]}."""
    path = os.path.join(run_dir, "metrics.csv")
    if not os.path.isfile(path):
        raise FileNotFoundError(f"{path} not found")
    with open(path, newline="") as f:
        reader = csv.reader(f)
        try:
            header = next(reader)
        except StopIteration:
            raise ValueError(f"{path} is empty")
        cols = {h: [] for h in header}
        for row in reader:
            if len(row) != len(header):
                continue
            for h, v in zip(header, row):
                cols[h].append(parse_num(v))
    missing = REQUIRED_COLUMNS - set(header)
    if missing:
        raise ValueError(f"{path} missing required columns: {sorted(missing)}")
    return cols


def parse_num(v):
    v = v.strip()
    if v == "":
        return None
    try:
        return float(v)
    except ValueError:
        return None


def series(cols, name):
    """Return (xs, ys) dropping rows where the value is unavailable."""
    xs, ys = [], []
    for x, y in zip(cols["elapsed_s"], cols.get(name, [None] * len(cols["elapsed_s"]))):
        if x is None or y is None or (isinstance(y, float) and math.isnan(y)):
            continue
        xs.append(x)
        ys.append(y)
    return xs, ys


def has_data(cols, name):
    return len(series(cols, name)[0]) > 0


def read_summary(run_dir):
    path = os.path.join(run_dir, "summary.json")
    if not os.path.isfile(path):
        return {}
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        print(f"warning: cannot read {path}: {e}", file=sys.stderr)
        return {}


def run_label(run_dir, summary):
    cfg = summary.get("config", {})
    label = summary.get("label") or ""
    mode = cfg.get("mode", "?")
    conc = cfg.get("concurrency", "?")
    base = os.path.basename(os.path.normpath(run_dir))
    parts = [label] if label else [base]
    parts.append(f"{mode} c{conc}")
    return " ".join(str(p) for p in parts)


def finish(fig, path, title):
    fig.suptitle(title, fontsize=11)
    fig.tight_layout(rect=(0, 0, 1, 0.96))
    fig.savefig(path, dpi=120)
    plt.close(fig)
    print(f"wrote {path}")


def plot_latency(run_dir, cols):
    plotted = False
    fig, ax = plt.subplots(figsize=(9, 5))
    for col, style in (("p50_ms", "-"), ("p95_ms", "--"), ("p99_ms", ":")):
        if has_data(cols, col):
            xs, ys = series(cols, col)
            ax.plot(xs, ys, style, label=col.replace("_ms", " (ms)"), linewidth=1.2)
            plotted = True
    if not plotted:
        plt.close(fig)
        print("skipped latency.png: no latency columns present")
        return
    ax.set_xlabel("Elapsed time (s)")
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Latency percentiles over time")
    ax.legend()
    ax.grid(True, alpha=0.3)
    finish(fig, os.path.join(run_dir, "latency.png"), "Latency percentiles")


def plot_throughput(run_dir, cols):
    fig, ax1 = plt.subplots(figsize=(9, 5))
    plotted = False
    if has_data(cols, "rps"):
        xs, ys = series(cols, "rps")
        ax1.plot(xs, ys, label="requests/s", color="tab:blue", linewidth=1.2)
        plotted = True
    ax1.set_xlabel("Elapsed time (s)")
    ax1.set_ylabel("HTTP requests per second", color="tab:blue")
    ax1.tick_params(axis="y", labelcolor="tab:blue")
    if has_data(cols, "payload_mib_s"):
        ax2 = ax1.twinx()
        xs, ys = series(cols, "payload_mib_s")
        ax2.plot(xs, ys, label="HTTP payload MiB/s", color="tab:orange", linewidth=1.2)
        ax2.set_ylabel("HTTP payload throughput (MiB/s)", color="tab:orange")
        ax2.tick_params(axis="y", labelcolor="tab:orange")
        plotted = True
    if has_data(cols, "logical_mib_s"):
        xs, ys = series(cols, "logical_mib_s")
        if not has_data(cols, "payload_mib_s"):
            ax2 = ax1.twinx()
        ax2.plot(xs, ys, label="logical MiB/s", color="tab:green", linestyle="--", linewidth=1.0)
        plotted = True
    if not plotted:
        plt.close(fig)
        print("skipped throughput.png: no throughput columns present")
        return
    ax1.set_title("Throughput over time")
    ax1.grid(True, alpha=0.3)
    lines, labels = ax1.get_legend_handles_labels()
    for a in fig.axes[1:]:
        l2, lab2 = a.get_legend_handles_labels()
        lines += l2
        labels += lab2
    ax1.legend(lines, labels, loc="best")
    finish(fig, os.path.join(run_dir, "throughput.png"), "Requests/s and payload throughput")


def plot_resources(run_dir, cols):
    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(9, 7), sharex=True)
    plotted = False
    if has_data(cols, "server_cpu_pct"):
        xs, ys = series(cols, "server_cpu_pct")
        ax1.plot(xs, ys, label="server process CPU (%)", color="tab:red", linewidth=1.2)
        plotted = True
    if has_data(cols, "server_system_cpu_pct"):
        xs, ys = series(cols, "server_system_cpu_pct")
        ax1.plot(xs, ys, label="system CPU (%)", color="tab:purple", linewidth=1.0)
        plotted = True
    if has_data(cols, "client_cpu_pct"):
        xs, ys = series(cols, "client_cpu_pct")
        ax1.plot(xs, ys, label="client process CPU (%)", color="tab:gray", linewidth=1.0)
        plotted = True
    ax1.set_ylabel("CPU utilization (%)")
    ax1.set_title("CPU utilization over time")
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    mem = False
    if has_data(cols, "server_rss_mib"):
        xs, ys = series(cols, "server_rss_mib")
        ax2.plot(xs, ys, label="server RSS (MiB)", color="tab:green", linewidth=1.2)
        mem = True
    if has_data(cols, "server_heap_mib"):
        xs, ys = series(cols, "server_heap_mib")
        ax2.plot(xs, ys, label="server Go heap (MiB)", color="tab:olive", linewidth=1.0)
        mem = True
    if has_data(cols, "client_rss_mib"):
        xs, ys = series(cols, "client_rss_mib")
        ax2.plot(xs, ys, label="client RSS (MiB)", color="tab:gray", linewidth=1.0)
        mem = True
    ax2.set_xlabel("Elapsed time (s)")
    ax2.set_ylabel("Memory (MiB)")
    ax2.set_title("Process memory over time")
    ax2.legend()
    ax2.grid(True, alpha=0.3)
    if not (plotted or mem):
        plt.close(fig)
        print("skipped resources.png: no CPU/RSS columns present")
        return
    finish(fig, os.path.join(run_dir, "resources.png"), "Server and client resources")


def scatter_vs(run_dir, cols, resource_col, resource_name, out_name, title):
    if not has_data(cols, resource_col):
        print(f"skipped {out_name}: column {resource_col} not available")
        return
    rx, ry = series(cols, resource_col)
    if not rx:
        print(f"skipped {out_name}: column {resource_col} not available")
        return
    # Join latency and resource samples on the same elapsed-second bucket.
    latency_at = {}
    for col in ("p50_ms", "p95_ms", "p99_ms"):
        for x, y in zip(*series(cols, col)):
            latency_at.setdefault(col, {})[int(x)] = y
    fig, ax = plt.subplots(figsize=(7, 5))
    plotted = False
    for col, marker in (("p50_ms", "o"), ("p95_ms", "s"), ("p99_ms", "^")):
        xs, ys = [], []
        m = latency_at.get(col, {})
        for x, y in zip(rx, ry):
            v = m.get(int(x))
            if v is not None:
                xs.append(y)
                ys.append(v)
        if xs:
            ax.scatter(xs, ys, s=14, alpha=0.6, marker=marker, label=col.replace("_ms", " (ms)"))
            plotted = True
    if not plotted:
        plt.close(fig)
        print(f"skipped {out_name}: no overlapping latency/resource samples")
        return
    ax.set_xlabel(resource_name)
    ax.set_ylabel("Latency (ms)")
    ax.set_title(title)
    ax.legend()
    ax.grid(True, alpha=0.3)
    finish(fig, os.path.join(run_dir, out_name), title)


def plot_system_activity(run_dir, cols):
    net = has_data(cols, "server_net_rx_mib_s") or has_data(cols, "server_net_tx_mib_s")
    disk = has_data(cols, "server_disk_read_mib_s") or has_data(cols, "server_disk_write_mib_s")
    mem = has_data(cols, "server_mem_available_mib")
    if not (net or disk or mem):
        print("skipped system_activity.png: no network/disk/memory columns present")
        return
    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(9, 7), sharex=True)
    if net or disk:
        for col, style, label in (
            ("server_net_rx_mib_s", "-", "net receive (MiB/s)"),
            ("server_net_tx_mib_s", "--", "net transmit (MiB/s)"),
            ("server_disk_read_mib_s", "-", "disk read (MiB/s)"),
            ("server_disk_write_mib_s", "--", "disk write (MiB/s)"),
        ):
            if has_data(cols, col):
                xs, ys = series(cols, col)
                ax1.plot(xs, ys, style, label=label, linewidth=1.1)
        ax1.set_ylabel("Throughput (MiB/s)")
        ax1.set_title("Network and disk throughput")
        ax1.legend()
        ax1.grid(True, alpha=0.3)
    else:
        ax1.text(0.5, 0.5, "no network/disk data", ha="center", va="center", transform=ax1.transAxes)
    if mem:
        xs, ys = series(cols, "server_mem_available_mib")
        ax2.plot(xs, ys, color="tab:blue", label="available memory (MiB)", linewidth=1.2)
        ax2.set_ylabel("Available memory (MiB)")
        ax2.set_title("System available memory")
        ax2.legend()
        ax2.grid(True, alpha=0.3)
    else:
        ax2.text(0.5, 0.5, "no memory data", ha="center", va="center", transform=ax2.transAxes)
    ax2.set_xlabel("Elapsed time (s)")
    finish(fig, os.path.join(run_dir, "system_activity.png"), "System activity")


def plot_gc_pressure(run_dir, cols):
    gc = has_data(cols, "server_gc_count") or has_data(cols, "server_gc_pause_ms")
    psi = has_data(cols, "server_psi_cpu_some_avg10") or has_data(cols, "server_psi_mem_some_avg10")
    if not (gc or psi):
        print("skipped gc_pressure.png: no GC/PSI columns present")
        return
    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(9, 7), sharex=True)
    if gc:
        if has_data(cols, "server_gc_count"):
            xs, ys = series(cols, "server_gc_count")
            # Cumulative counter: plot per-interval deltas.
            deltas = [None] + [b - a for a, b in zip(ys, ys[1:])]
            dx = [x for x, d in zip(xs, deltas) if d is not None]
            dd = [d for d in deltas if d is not None]
            ax1.bar(dx, dd, width=0.8, color="tab:purple", alpha=0.7, label="GC cycles per interval")
        if has_data(cols, "server_gc_pause_ms"):
            xs, ys = series(cols, "server_gc_pause_ms")
            ax1.plot(xs, ys, color="tab:red", label="GC pause (ms per interval)", linewidth=1.2)
        ax1.set_ylabel("GC activity")
        ax1.set_title("Garbage collection pressure")
        ax1.legend()
        ax1.grid(True, alpha=0.3)
    else:
        ax1.text(0.5, 0.5, "no GC data", ha="center", va="center", transform=ax1.transAxes)
    if psi:
        for col, label in (
            ("server_psi_cpu_some_avg10", "CPU PSI some avg10 (%)"),
            ("server_psi_mem_some_avg10", "memory PSI some avg10 (%)"),
        ):
            if has_data(cols, col):
                xs, ys = series(cols, col)
                ax2.plot(xs, ys, label=label, linewidth=1.1)
        ax2.set_ylabel("Pressure stall information (%)")
        ax2.set_title("PSI (contention experienced by tasks)")
        ax2.legend()
        ax2.grid(True, alpha=0.3)
    else:
        ax2.text(0.5, 0.5, "no PSI data (kernel or platform unsupported)",
                 ha="center", va="center", transform=ax2.transAxes)
    ax2.set_xlabel("Elapsed time (s)")
    finish(fig, os.path.join(run_dir, "gc_pressure.png"), "GC and PSI")


def plot_hardware(run_dir, cols):
    """PMU counters and page-fault/swap activity (hardware evidence)."""
    has_ipc = has_data(cols, "server_ipc")
    has_miss = has_data(cols, "server_cache_misses_per_s") or has_data(cols, "server_cache_miss_rate")
    has_fault = has_data(cols, "server_page_faults_per_s") or has_data(cols, "server_major_faults_per_s")
    has_swap = has_data(cols, "server_swap_in_mib_s") or has_data(cols, "server_swap_out_mib_s")
    if not (has_ipc or has_miss or has_fault or has_swap):
        print("skipped hardware.png: no PMU/fault/swap columns present")
        return

    panels = []
    if has_ipc:
        panels.append(("ipc", "Instruction throughput (IPC)"))
    if has_miss:
        panels.append(("miss", "Cache misses (PMU)"))
    if has_fault:
        panels.append(("faults", "Process page faults"))
    if has_swap:
        panels.append(("swap", "System swap activity"))
    if not panels:
        print("skipped hardware.png: no PMU/fault/swap columns present")
        return

    fig, axes = plt.subplots(len(panels), 1, figsize=(9, 3 * len(panels) + 1), sharex=True)
    if len(panels) == 1:
        axes = [axes]
    for ax, (kind, title) in zip(axes, panels):
        if kind == "ipc":
            if has_data(cols, "server_ipc"):
                xs, ys = series(cols, "server_ipc")
                ax.plot(xs, ys, label="IPC (instructions/cycle)", color="tab:blue", linewidth=1.2)
            ax.set_ylabel("IPC (instr/cycle)")
        elif kind == "miss":
            drawn = False
            if has_data(cols, "server_cache_misses_per_s"):
                xs, ys = series(cols, "server_cache_misses_per_s")
                ax.plot(xs, ys, label="cache misses/s", color="tab:red", linewidth=1.1)
                ax.set_ylabel("misses / s")
                drawn = True
            if has_data(cols, "server_cache_miss_rate"):
                ax2 = ax.twinx()
                xs, ys = series(cols, "server_cache_miss_rate")
                ax2.plot(xs, ys, label="miss rate (misses/refs)", color="tab:orange",
                         linestyle="--", linewidth=1.1)
                ax2.set_ylabel("miss rate (ratio)")
                ax2.set_ylim(0, 1)
                drawn = True
            if not drawn:
                ax.text(0.5, 0.5, "no PMU data (VM without counter passthrough?)",
                        ha="center", va="center", transform=ax.transAxes)
        elif kind == "faults":
            for col, label, color in (
                ("server_page_faults_per_s", "minor faults/s", "tab:green"),
                ("server_major_faults_per_s", "major faults/s", "tab:red"),
            ):
                if has_data(cols, col):
                    xs, ys = series(cols, col)
                    ax.plot(xs, ys, label=label, color=color, linewidth=1.1)
            ax.set_ylabel("faults / s")
        elif kind == "swap":
            for col, label, color in (
                ("server_swap_in_mib_s", "swap in (MiB/s)", "tab:blue"),
                ("server_swap_out_mib_s", "swap out (MiB/s)", "tab:red"),
            ):
                if has_data(cols, col):
                    xs, ys = series(cols, col)
                    ax.plot(xs, ys, label=label, color=color, linewidth=1.1)
            ax.set_ylabel("swap (MiB/s)")
        ax.set_title(title)
        ax.legend(fontsize=8)
        ax.grid(True, alpha=0.3)
    axes[-1].set_xlabel("Elapsed time (s)")
    finish(fig, os.path.join(run_dir, "hardware.png"), "Hardware counters (PMU, faults, swap)")


def generate_single(run_dir):
    print(f"== generating plots for {run_dir} ==")
    try:
        cols = read_metrics(run_dir)
    except (OSError, ValueError) as e:
        print(f"error: {e}", file=sys.stderr)
        return False
    summary = read_summary(run_dir)
    title = run_label(run_dir, summary)
    plot_latency(run_dir, cols)
    plot_throughput(run_dir, cols)
    plot_resources(run_dir, cols)
    scatter_vs(run_dir, cols, "server_cpu_pct", "Server CPU utilization (%)",
               "latency_vs_cpu.png", f"Latency vs CPU — {title}")
    scatter_vs(run_dir, cols, "server_rss_mib", "Server RSS (MiB)",
               "latency_vs_memory.png", f"Latency vs RSS — {title}")
    plot_system_activity(run_dir, cols)
    plot_gc_pressure(run_dir, cols)
    plot_hardware(run_dir, cols)
    return True


def load_run(run_dir):
    cols = read_metrics(run_dir)
    summary = read_summary(run_dir)
    return run_dir, cols, summary


def compare_lines(ax, runs, col, ylabel, title):
    plotted = False
    for run_dir, cols, summary in runs:
        if has_data(cols, col):
            xs, ys = series(cols, col)
            ax.plot(xs, ys, label=run_label(run_dir, summary), linewidth=1.2)
            plotted = True
    ax.set_xlabel("Elapsed time (s)")
    ax.set_ylabel(ylabel)
    ax.set_title(title)
    ax.grid(True, alpha=0.3)
    if plotted:
        ax.legend(fontsize=8)
    return plotted


def generate_compare(run_dirs, out_dir, labels):
    runs = []
    for d in run_dirs:
        try:
            runs.append(load_run(d))
        except (OSError, ValueError) as e:
            print(f"warning: skipping {d}: {e}", file=sys.stderr)
    if not runs:
        print("error: no readable run directories", file=sys.stderr)
        return False
    if labels and len(labels) == len(runs):
        for (d, _, s), lab in zip(runs, labels):
            s.setdefault("label", "")
            s["label"] = lab
    os.makedirs(out_dir, exist_ok=True)

    # Latency percentiles: one panel per percentile, one line per run.
    fig, axes = plt.subplots(3, 1, figsize=(9, 9), sharex=True)
    any_latency = False
    for ax, col, name in zip(axes, ("p50_ms", "p95_ms", "p99_ms"), ("p50", "p95", "p99")):
        if compare_lines(ax, runs, col, f"{name} latency (ms)", f"{name} latency over time"):
            any_latency = True
        else:
            ax.text(0.5, 0.5, f"no {name} data", ha="center", va="center", transform=ax.transAxes)
    if any_latency:
        fig.suptitle("Latency comparison", fontsize=11)
        fig.tight_layout(rect=(0, 0, 1, 0.97))
        path = os.path.join(out_dir, "compare_latency.png")
        fig.savefig(path, dpi=120)
        print(f"wrote {path}")
    plt.close(fig)

    # Throughput: requests/s and payload MiB/s.
    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(9, 7), sharex=True)
    ok1 = compare_lines(ax1, runs, "rps", "HTTP requests per second", "Requests per second")
    ok2 = compare_lines(ax2, runs, "payload_mib_s", "HTTP payload (MiB/s)", "Payload throughput")
    if not ok1:
        ax1.text(0.5, 0.5, "no rps data", ha="center", va="center", transform=ax1.transAxes)
    if not ok2:
        ax2.text(0.5, 0.5, "no throughput data", ha="center", va="center", transform=ax2.transAxes)
    fig.suptitle("Throughput comparison", fontsize=11)
    fig.tight_layout(rect=(0, 0, 1, 0.97))
    path = os.path.join(out_dir, "compare_throughput.png")
    fig.savefig(path, dpi=120)
    print(f"wrote {path}")
    plt.close(fig)

    # CPU and memory.
    for col, ylabel, title, name in (
        ("server_cpu_pct", "Server CPU utilization (%)", "CPU utilization", "compare_cpu.png"),
        ("server_rss_mib", "Server RSS (MiB)", "Process memory usage", "compare_memory.png"),
    ):
        fig, ax = plt.subplots(figsize=(9, 5))
        if compare_lines(ax, runs, col, ylabel, title):
            fig.suptitle(f"{title} comparison", fontsize=11)
            fig.tight_layout(rect=(0, 0, 1, 0.96))
            path = os.path.join(out_dir, name)
            fig.savefig(path, dpi=120)
            print(f"wrote {path}")
        else:
            print(f"skipped {name}: column {col} not present in any run")
        plt.close(fig)
    return True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("paths", nargs="+",
                        help="run directory, or 'compare' followed by run directories")
    parser.add_argument("-o", "--output", help="output directory for comparison plots")
    parser.add_argument("--labels", help="comma-separated labels for compared runs")
    args = parser.parse_args(argv)

    paths = args.paths
    if paths and paths[0] == "compare":
        run_dirs = paths[1:]
        if len(run_dirs) < 2:
            print("error: compare mode needs at least two run directories", file=sys.stderr)
            return 2
        labels = args.labels.split(",") if args.labels else []
        out = args.output or os.path.join(os.path.commonpath(run_dirs), "comparison")
        return 0 if generate_compare(run_dirs, out, labels) else 1

    ok = True
    for d in paths:
        ok = generate_single(d) and ok
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
