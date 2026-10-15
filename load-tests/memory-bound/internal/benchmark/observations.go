package benchmark

import "fmt"

// Observe produces a concise, evidence-based summary of a finished run.
//
// The statements are possible explanations, not definitive conclusions:
// correlation between GC activity and latency does not prove causation,
// and neither dataset size nor CPU utilization alone identifies a memory
// bandwidth bottleneck. Each observation names the evidence and, where
// useful, suggests a follow-up experiment.
func Observe(r *Run) []string {
	var out []string
	s := r.Result.Summary

	if s.Requests == 0 {
		return []string{"No requests completed; nothing to interpret."}
	}

	// 1. Error rate.
	if s.Errors > 0 {
		rate := float64(s.Errors) / float64(s.Requests) * 100
		out = append(out, fmt.Sprintf(
			"%.2f%% of requests failed (%d of %d). Latency percentiles include these failed requests; inspect server logs and metrics.csv error column before drawing throughput conclusions.",
			rate, s.Errors, s.Requests))
	}

	// 2. Client-side saturation (load generator limiting).
	avgClientCPU, clientSamples := meanClientCPU(r.Client)
	if clientSamples >= 3 && avgClientCPU >= 90 {
		out = append(out, fmt.Sprintf(
			"EVIDENCE: client process CPU averaged %.1f%% of a single core over %d samples. POSSIBLE EXPLANATION: the load generator itself is saturated and may be limiting measured throughput. FOLLOW-UP: rerun the same workload with lower concurrency or from a larger client instance and compare achieved requests/s.",
			avgClientCPU, clientSamples))
	}

	// 3. Latency vs CPU trend within the run.
	if obs := trendObs(r); obs != "" {
		out = append(out, obs)
	}

	// 4. GC pauses coinciding with latency spikes.
	if obs := gcObs(r); obs != "" {
		out = append(out, obs)
	}

	// 5. Server memory growth.
	if obs := memoryObs(r); obs != "" {
		out = append(out, obs)
	}

	// 6. Unexpected disk I/O in an in-memory workload.
	if obs := diskObs(r); obs != "" {
		out = append(out, obs)
	}

	// 6b. Swap traffic: the strongest in-guest sign of memory exhaustion.
	if obs := swapObs(r); obs != "" {
		out = append(out, obs)
	}

	// 6c. Hardware counters: cache miss rate and IPC.
	if obs := hwObs(r); obs != "" {
		out = append(out, obs)
	}

	// 7. PSI pressure.
	if obs := psiObs(r); obs != "" {
		out = append(out, obs)
	}

	// 8. Rate limiting note.
	if r.Cfg.Rate > 0 {
		rel := s.RPS / r.Cfg.Rate * 100
		out = append(out, fmt.Sprintf(
			"Target rate was %.0f req/s; achieved %.0f req/s (%.0f%% of target). When the target is met, latency and resource use reflect the fixed rate, not the server's maximum throughput.",
			r.Cfg.Rate, s.RPS, rel))
	}

	if len(out) == 0 {
		out = append(out, fmt.Sprintf(
			"Run completed with %.0f req/s, p50=%.2f ms, p99=%.2f ms. No single-sample anomaly stood out; compare against other concurrency levels or instance types using the comparison mode to detect scaling limits.",
			s.RPS, s.P50Ms, s.P99Ms))
	}
	return out
}

func meanClientCPU(cs []ClientSample) (float64, int) {
	var sum float64
	n := 0
	for _, s := range cs {
		if s.CPUOK {
			sum += s.CPUPct
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// trendObs compares first-third and last-third p99 and CPU.
func trendObs(r *Run) string {
	iv := r.Result.Intervals
	n := len(iv)
	if n < 6 {
		return ""
	}
	third := n / 3
	firstP99 := meanP99(iv[:third])
	lastP99 := meanP99(iv[n-third:])
	if firstP99 <= 0 || lastP99 < 1.5*firstP99 {
		return ""
	}
	firstCPU := meanServerCPU(r.Server[:minInt(len(r.Server), third)])
	lastCPU := meanServerCPU(r.Server[maxInt(0, len(r.Server)-third):])
	msg := fmt.Sprintf(
		"EVIDENCE: p99 latency rose from %.2f ms (first third) to %.2f ms (last third, +%.0f%%).",
		firstP99, lastP99, (lastP99/firstP99-1)*100)
	if firstCPU > 0 && lastCPU > firstCPU*1.2 {
		msg += fmt.Sprintf(" Server CPU also rose from %.1f%% to %.1f%%, which can explain part of the latency increase.", firstCPU, lastCPU)
	} else {
		msg += fmt.Sprintf(" Server CPU stayed near %.1f%%. FOLLOW-UP: check GC, memory growth, and client saturation before attributing this to the server.", lastCPU)
	}
	return "POSSIBLE EXPLANATION: latency degraded during the run. " + msg
}

// gcObs flags intervals where notable GC pauses overlap top latency spikes.
func gcObs(r *Run) string {
	var maxPause float64
	pauseAt := map[int]float64{}
	for _, s := range r.Server {
		if s.GCPause != nil && *s.GCPause > maxPause {
			maxPause = *s.GCPause
		}
		if s.GCPause != nil {
			pauseAt[int(timeSec(r, s.UnixNano))] = *s.GCPause
		}
	}
	if maxPause < 50 || len(r.Result.Intervals) == 0 {
		return ""
	}
	// Find the worst latency interval.
	worst := 0
	for i, iv := range r.Result.Intervals {
		if iv.P99Ms > r.Result.Intervals[worst].P99Ms {
			worst = i
		}
	}
	worstP99 := r.Result.Intervals[worst].P99Ms
	p := pauseAt[worst]
	rel := ""
	if p > 0 {
		rel = fmt.Sprintf(" The worst-latency interval (p99 %.2f ms at t=%ds) coincided with a %.1f ms GC pause in that same second.", worstP99, worst, p)
	} else {
		rel = fmt.Sprintf(" The worst-latency interval had p99 %.2f ms at t=%ds with no GC pause recorded in the same second.", worstP99, worst)
	}
	return fmt.Sprintf(
		"EVIDENCE: maximum GC pause in any interval was %.1f ms.%s CORRELATION IS NOT CAUSATION: to test whether GC drives the tail, rerun with GOGC=off or a much larger GOGC and compare p99.",
		maxPause, rel)
}

// memoryObs reports significant server RSS growth during the run.
func memoryObs(r *Run) string {
	first, ok1 := firstRSS(r.Server)
	last, ok2 := lastRSS(r.Server)
	if !ok1 || !ok2 || first <= 0 {
		return ""
	}
	growth := (last/first - 1) * 100
	if growth < 25 {
		return ""
	}
	return fmt.Sprintf(
		"EVIDENCE: server RSS grew from %.1f MiB to %.1f MiB (+%.0f%%) during the measurement. POSSIBLE EXPLANATIONS: allocation-heavy workload, Go heap growth, or the dataset changing size. FOLLOW-UP: check gc_pressure.png and rerun with a fixed dataset to see whether memory stabilizes.",
		first, last, growth)
}

// diskObs flags non-trivial whole-system disk throughput.
func diskObs(r *Run) string {
	var total float64
	samples := 0
	for _, s := range r.Server {
		if s.DiskRead != nil && s.DiskWrite != nil {
			total += *s.DiskRead + *s.DiskWrite
			samples++
		}
	}
	if samples == 0 {
		return ""
	}
	avg := total / float64(samples)
	if avg < 1.0 {
		return ""
	}
	return fmt.Sprintf(
		"EVIDENCE: whole-system disk throughput averaged %.2f MiB/s during the run even though the store is memory-only. POSSIBLE EXPLANATIONS: unrelated system activity or logging elsewhere on the host. FOLLOW-UP: repeat on an idle instance; if it persists, correlate with other tenants or OS services.",
		avg)
}

// psiObs reports sustained pressure-stall information when available.
func psiObs(r *Run) string {
	cpu, nCPU := meanPSI(r.Server, func(s ServerSample) *float64 { return s.PSICPU })
	mem, nMem := meanPSI(r.Server, func(s ServerSample) *float64 { return s.PSIMem })
	switch {
	case nMem >= 3 && mem >= 5:
		return fmt.Sprintf(
			"EVIDENCE: memory PSI \"some\" avg10 averaged %.1f%% over %d samples. The system was stalling on memory for a noticeable fraction of time. FOLLOW-UP: compare against a run on a larger-memory instance or with a smaller dataset.",
			mem, nMem)
	case nCPU >= 3 && cpu >= 10:
		return fmt.Sprintf(
			"EVIDENCE: CPU PSI \"some\" avg10 averaged %.1f%% over %d samples, indicating CPU contention on the host. FOLLOW-UP: compare against a CPU-larger instance type.",
			cpu, nCPU)
	}
	return ""
}

func meanP99(iv []Interval) float64 {
	if len(iv) == 0 {
		return 0
	}
	var sum float64
	for _, v := range iv {
		sum += v.P99Ms
	}
	return sum / float64(len(iv))
}

// swapObs flags any sustained swap traffic: the guest is out of RAM.
func swapObs(r *Run) string {
	var sumIn, sumOut, peak float64
	n := 0
	for _, s := range r.Server {
		if s.SwapIn == nil || s.SwapOut == nil {
			continue
		}
		sumIn += *s.SwapIn
		sumOut += *s.SwapOut
		if *s.SwapIn > peak {
			peak = *s.SwapIn
		}
		if *s.SwapOut > peak {
			peak = *s.SwapOut
		}
		n++
	}
	if n == 0 {
		return ""
	}
	avg := (sumIn + sumOut) / float64(n)
	if avg < 0.01 {
		return ""
	}
	return fmt.Sprintf(
		"EVIDENCE: the guest recorded swap traffic (avg %.2f MiB/s in+out, peak %.2f MiB/s) while serving an in-memory workload -- the direct signature of a memory-capacity bottleneck, not a CPU limit. POSSIBLE EXPLANATION: the dataset plus runtime overhead exceeds physical RAM. FOLLOW-UP: rerun with a smaller dataset or more assigned RAM and confirm swap traffic stops and p99 recovers.",
		avg, peak)
}

// hwObs reports hardware performance counters (PMU): cache miss rate and
// instructions per cycle. Available on bare metal and most cloud guests;
// absent inside VirtualBox guests (use scripts/host_perf.sh on the host).
func hwObs(r *Run) string {
	missRate, nMR := meanPSI(r.Server, func(s ServerSample) *float64 { return s.CacheMissRate })
	ipc, nIPC := meanPSI(r.Server, func(s ServerSample) *float64 { return s.IPC })
	if nMR >= 3 && missRate >= 0.10 {
		return fmt.Sprintf(
			"EVIDENCE: hardware counters show a cache miss rate of %.1f%% (misses/references) with IPC %.2f over %d samples. A request path that only hashes a map stalls this much on memory when the working set outgrows cache. POSSIBLE EXPLANATION: memory-bound execution. FOLLOW-UP: compare with the CPU-bound reference app on the same instance; if the guest PMU is unavailable, run perf stat on the host (scripts/host_perf.sh).",
			missRate*100, ipc, nMR)
	}
	if nIPC >= 3 && ipc > 0 && ipc < 0.8 {
		return fmt.Sprintf(
			"EVIDENCE: server IPC averaged %.2f over %d samples -- instructions are stalling rather than retiring promptly. Consistent with memory-stall-heavy execution (correlation, not proof). FOLLOW-UP: compare against a CPU-bound workload on identical hardware; higher IPC there would corroborate the memory bottleneck.",
			ipc, nIPC)
	}
	return ""
}

func meanServerCPU(ss []ServerSample) float64 {
	var sum float64
	n := 0
	for _, s := range ss {
		if s.CPU != nil {
			sum += *s.CPU
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func firstRSS(ss []ServerSample) (float64, bool) {
	for _, s := range ss {
		if s.RSS != nil {
			return *s.RSS, true
		}
	}
	return 0, false
}

func lastRSS(ss []ServerSample) (float64, bool) {
	for i := len(ss) - 1; i >= 0; i-- {
		if ss[i].RSS != nil {
			return *ss[i].RSS, true
		}
	}
	return 0, false
}

func meanPSI(ss []ServerSample, get func(ServerSample) *float64) (float64, int) {
	var sum float64
	n := 0
	for _, s := range ss {
		if v := get(s); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

func timeSec(r *Run, unixNano int64) float64 {
	return float64(unixNano-r.MeasureStart.UnixNano()) / 1e9
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
