package benchmark

import (
	"testing"
	"time"
)

func mkSample(base time.Time, offsetMs, durMs float64, values int, ok, write bool, in, out int) Sample {
	return Sample{
		T:        base.Add(time.Duration(offsetMs) * time.Millisecond),
		Dur:      time.Duration(durMs * float64(time.Millisecond)),
		Values:   values,
		OK:       ok,
		Write:    write,
		BytesIn:  in,
		BytesOut: out,
	}
}

func TestSummarizePercentiles(t *testing.T) {
	base := time.Now()
	// 100 samples: 1..100 ms, all in interval 0.
	l := &WorkerLog{}
	for i := 1; i <= 100; i++ {
		l.Add(mkSample(base, float64(i), float64(i), 1, true, false, 0, 10))
	}
	res := Summarize([]*WorkerLog{l}, base, base.Add(time.Second), 10, 4)
	s := res.Summary
	if s.Requests != 100 || s.Success != 100 || s.Errors != 0 {
		t.Fatalf("counts = %d/%d/%d", s.Requests, s.Success, s.Errors)
	}
	// Nearest-rank: p50 = rank 50 = 50ms; p95 = rank 95 = 95ms; p99 = 99ms.
	if s.P50Ms != 50 || s.P95Ms != 95 || s.P99Ms != 99 {
		t.Fatalf("p50/p95/p99 = %v/%v/%v; want 50/95/99", s.P50Ms, s.P95Ms, s.P99Ms)
	}
	if s.MinMs != 1 || s.MaxMs != 100 {
		t.Fatalf("min/max = %v/%v; want 1/100", s.MinMs, s.MaxMs)
	}
	if s.AvgMs != 50.5 {
		t.Fatalf("avg = %v; want 50.5", s.AvgMs)
	}
	if len(res.Intervals) != 1 {
		t.Fatalf("intervals = %d; want 1", len(res.Intervals))
	}
	if got := res.Intervals[0].Requests; got != 100 {
		t.Fatalf("interval requests = %d", got)
	}
}

func TestSummarizeFailedRequestsNotDropped(t *testing.T) {
	base := time.Now()
	l := &WorkerLog{}
	l.Add(mkSample(base, 0, 5, 1, true, false, 0, 10))
	l.Add(mkSample(base, 10, 500, 1, false, false, 0, 0)) // failed slow request
	res := Summarize([]*WorkerLog{l}, base, base.Add(time.Second), 10, 1)
	s := res.Summary
	if s.Requests != 2 || s.Success != 1 || s.Errors != 1 {
		t.Fatalf("counts = %d/%d/%d", s.Requests, s.Success, s.Errors)
	}
	if s.MaxMs != 500 {
		t.Fatalf("max = %v; want 500 (failed request latency retained)", s.MaxMs)
	}
	if s.Gets != 2 || s.Puts != 0 {
		t.Fatalf("gets/puts = %d/%d", s.Gets, s.Puts)
	}
}

func TestSummarizeIntervalBucketingAndRatio(t *testing.T) {
	base := time.Now()
	l := &WorkerLog{}
	// 3 seconds of traffic: reads and writes.
	for sec := 0; sec < 3; sec++ {
		for i := 0; i < 10; i++ {
			ok := true
			write := i%2 == 1
			l.Add(mkSample(base, float64(sec*1000+i*10), 8, 1, ok, write, 20, 0))
		}
	}
	res := Summarize([]*WorkerLog{l}, base, base.Add(3*time.Second), 20, 2)
	if len(res.Intervals) != 3 {
		t.Fatalf("intervals = %d; want 3", len(res.Intervals))
	}
	for i, iv := range res.Intervals {
		if iv.Requests != 10 {
			t.Fatalf("interval %d requests = %d; want 10", i, iv.Requests)
		}
		if iv.Gets != 5 || iv.Puts != 5 {
			t.Fatalf("interval %d gets/puts = %d/%d", i, iv.Gets, iv.Puts)
		}
	}
	s := res.Summary
	if s.ReadRatio != 50 {
		t.Fatalf("read ratio = %v; want 50", s.ReadRatio)
	}
	if s.RPS != 10 {
		t.Fatalf("rps = %v; want 10", s.RPS)
	}
	if s.ValuesPerSec != 10 {
		t.Fatalf("values/s = %v; want 10", s.ValuesPerSec)
	}
	// payload: 10 req/s * 20 bytes in = 200 B/s
	if s.PayloadMiBs <= 0 {
		t.Fatal("payload MiB/s should be > 0")
	}
	if s.LogicalMiBs <= 0 {
		t.Fatal("logical MiB/s should be > 0")
	}
	if s.Concurrency != 2 {
		t.Fatalf("concurrency = %d", s.Concurrency)
	}
}

func TestSummarizeDropsTrailingPartialInterval(t *testing.T) {
	base := time.Now()
	l := &WorkerLog{}
	for i := 0; i < 5; i++ {
		l.Add(mkSample(base, float64(i*200), 5, 1, true, false, 0, 10)) // seconds 0-0.8
	}
	// One sample in the partial second 1.0-1.2 of a 1.2s window.
	l.Add(mkSample(base, 1100, 5, 1, true, false, 0, 10))
	res := Summarize([]*WorkerLog{l}, base, base.Add(1200*time.Millisecond), 10, 1)
	if len(res.Intervals) != 1 {
		t.Fatalf("intervals = %d; want 1 (partial tail dropped)", len(res.Intervals))
	}
	if res.Summary.Requests != 6 {
		t.Fatalf("summary must still include the dropped sample: %d", res.Summary.Requests)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	base := time.Now()
	res := Summarize([]*WorkerLog{{}}, base, base.Add(time.Second), 8, 1)
	if res.Summary.Requests != 0 || len(res.Intervals) != 0 {
		t.Fatal("empty log should yield empty result")
	}
}

func TestParseFrames(t *testing.T) {
	// two frames: "abc" and "de"
	body := []byte{0, 0, 0, 3, 'a', 'b', 'c', 0, 0, 0, 2, 'd', 'e'}
	n, bad := parseFrames(body)
	if n != 2 || bad {
		t.Fatalf("n=%d bad=%v", n, bad)
	}
	if _, bad := parseFrames([]byte{0, 0}); bad != true {
		t.Fatal("truncated header not detected")
	}
	if _, bad := parseFrames([]byte{0, 0, 0, 10, 'x'}); bad != true {
		t.Fatal("truncated payload not detected")
	}
}
