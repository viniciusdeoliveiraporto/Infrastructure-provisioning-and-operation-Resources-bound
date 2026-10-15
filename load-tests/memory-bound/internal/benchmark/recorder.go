package benchmark

import (
	"math"
	"sort"
	"time"
)

// Sample is one recorded HTTP operation from the load generator.
type Sample struct {
	T        time.Time // completion time (used for interval bucketing)
	Dur      time.Duration
	BytesIn  int // request body bytes sent
	BytesOut int // response body bytes received
	Values   int // logical values accessed (1 for point ops, n for batch)
	OK       bool
	Write    bool
}

// WorkerLog accumulates samples of a single worker without locking on the
// hot path. Logs are merged once after the run.
type WorkerLog struct {
	Samples []Sample
}

// Add appends one sample.
func (l *WorkerLog) Add(s Sample) {
	l.Samples = append(l.Samples, s)
}

// Interval holds per-second aggregates of the measurement window.
type Interval struct {
	ElapsedS float64 // seconds since measurement start (bucket start)
	Requests uint64
	Errors   uint64
	Gets     uint64
	Puts     uint64
	Values   uint64
	BytesIn  uint64
	BytesOut uint64
	AvgMs    float64
	P50Ms    float64
	P95Ms    float64
	P99Ms    float64
	MinMs    float64
	MaxMs    float64
}

// Summary holds whole-run aggregates over the measurement window.
type Summary struct {
	Requests  uint64
	Success   uint64
	Errors    uint64
	Gets      uint64
	Puts      uint64
	Values    uint64
	BytesIn   uint64
	BytesOut  uint64
	DurationS float64

	RPS          float64 // HTTP requests per second
	ValuesPerSec float64 // logical values accessed per second
	PayloadMiBs  float64 // HTTP payload MiB/s (bytes in+out)
	LogicalMiBs  float64 // estimated logical bytes accessed MiB/s

	AvgMs float64
	P50Ms float64
	P95Ms float64
	P99Ms float64
	MinMs float64
	MaxMs float64

	ReadRatio   float64 // effective GET/(GET+PUT) percent
	Concurrency int
}

// Result is the complete outcome of a measurement run.
type Result struct {
	Intervals []Interval
	Summary   Summary
}

// Summarize merges worker logs recorded during [measureStart, measureEnd).
// Latency percentiles include failed requests (errors are counted
// separately); dropping them would hide tail latency caused by failures.
//
// Percentile method: nearest-rank on the sorted sample durations —
// p50 of N samples is the element at rank ceil(0.50*N) (1-based).
func Summarize(logs []*WorkerLog, measureStart, measureEnd time.Time, valueSize, concurrency int) Result {
	window := measureEnd.Sub(measureStart)
	if window <= 0 {
		window = time.Nanosecond
	}
	buckets := make(map[int][]time.Duration)
	var res Summary
	for _, l := range logs {
		for _, s := range l.Samples {
			res.Requests++
			res.BytesIn += uint64(s.BytesIn)
			res.BytesOut += uint64(s.BytesOut)
			res.Values += uint64(s.Values)
			if s.Write {
				res.Puts++
			} else {
				res.Gets++
			}
			if s.OK {
				res.Success++
			} else {
				res.Errors++
			}
			idx := int(s.T.Sub(measureStart) / time.Second)
			if idx < 0 {
				idx = 0
			}
			buckets[idx] = append(buckets[idx], s.Dur)
		}
	}
	res.DurationS = window.Seconds()
	res.Concurrency = concurrency
	if res.Requests > 0 {
		res.RPS = float64(res.Requests) / res.DurationS
		res.ValuesPerSec = float64(res.Values) / res.DurationS
		res.PayloadMiBs = float64(res.BytesIn+res.BytesOut) / (1 << 20) / res.DurationS
		res.LogicalMiBs = float64(res.Values) * float64(valueSize) / (1 << 20) / res.DurationS
		if res.Gets+res.Puts > 0 {
			res.ReadRatio = float64(res.Gets) / float64(res.Gets+res.Puts) * 100
		}
	}

	// Global latency stats over all recorded samples.
	all := make([]time.Duration, 0, res.Requests)
	for _, durs := range buckets {
		all = append(all, durs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	res.AvgMs, res.P50Ms, res.P95Ms, res.P99Ms, res.MinMs, res.MaxMs = momentStats(all)

	// Per-interval stats.
	nBuckets := 0
	for k := range buckets {
		if k+1 > nBuckets {
			nBuckets = k + 1
		}
	}
	intervals := make([]Interval, 0, nBuckets)
	for i := 0; i < nBuckets; i++ {
		durs := buckets[i]
		if len(durs) == 0 {
			intervals = append(intervals, Interval{ElapsedS: float64(i)})
			continue
		}
		sort.Slice(durs, func(a, b int) bool { return durs[a] < durs[b] })
		avg, p50, p95, p99, min, max := momentStats(durs)
		intervals = append(intervals, Interval{
			ElapsedS: float64(i),
			AvgMs:    avg, P50Ms: p50, P95Ms: p95, P99Ms: p99,
			MinMs: min, MaxMs: max,
		})
	}
	// Second pass: counts per interval (needs the sample stream again).
	for _, l := range logs {
		for _, s := range l.Samples {
			idx := int(s.T.Sub(measureStart) / time.Second)
			if idx < 0 {
				idx = 0
			}
			if idx >= len(intervals) {
				continue
			}
			iv := &intervals[idx]
			iv.Requests++
			iv.BytesIn += uint64(s.BytesIn)
			iv.BytesOut += uint64(s.BytesOut)
			iv.Values += uint64(s.Values)
			if s.Write {
				iv.Puts++
			} else {
				iv.Gets++
			}
			if !s.OK {
				iv.Errors++
			}
		}
	}
	// Drop the trailing partial interval: measurement usually stops
	// mid-second, and a partially filled bucket would show an artificial
	// latency/throughput drop. Runs shorter than one second keep it.
	for len(intervals) > 1 {
		last := intervals[len(intervals)-1]
		if window.Seconds()-last.ElapsedS >= 0.9 {
			break
		}
		intervals = intervals[:len(intervals)-1]
	}
	return Result{Intervals: intervals, Summary: res}
}

// momentStats computes avg and nearest-rank percentiles of an ascending
// sorted duration slice.
func momentStats(sorted []time.Duration) (avg, p50, p95, p99, min, max float64) {
	n := len(sorted)
	if n == 0 {
		return 0, 0, 0, 0, 0, 0
	}
	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	avg = float64(sum) / float64(n) / 1e6
	min = float64(sorted[0]) / 1e6
	max = float64(sorted[n-1]) / 1e6
	p50 = float64(nearestRank(sorted, 0.50)) / 1e6
	p95 = float64(nearestRank(sorted, 0.95)) / 1e6
	p99 = float64(nearestRank(sorted, 0.99)) / 1e6
	return
}

func nearestRank(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
