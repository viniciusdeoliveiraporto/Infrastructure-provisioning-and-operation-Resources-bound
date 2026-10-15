package monitor

import (
	"testing"
	"time"
)

func TestSampleOnceFirstSampleRatesUnavailable(t *testing.T) {
	m := New(time.Second, 10)
	s := m.SampleOnce(time.Now())
	if !s.HeapOK {
		t.Fatal("heap metric unavailable")
	}
	if s.CPUOK || s.SystemOK || s.DiskOK || s.NetOK {
		t.Fatal("rate metrics must be unavailable on the first sample")
	}
	if !s.GCOK || s.GCPauseMs != 0 {
		t.Fatalf("GC = ok=%v pause=%v; want ok=true pause=0", s.GCOK, s.GCPauseMs)
	}
}

func TestSampleOnceSecondSampleRatesAvailable(t *testing.T) {
	m := New(time.Second, 10)
	m.SampleOnce(time.Now())
	time.Sleep(50 * time.Millisecond)
	s := m.SampleOnce(time.Now())
	if !s.CPUOK {
		t.Fatal("process CPU should be available on Linux after a baseline sample")
	}
	if s.CPUPct < 0 || s.CPUPct > 1000 {
		t.Fatalf("implausible CPU%%: %v", s.CPUPct)
	}
	if !s.RSSOK || s.RSSMiB <= 0 {
		t.Fatalf("RSS unavailable or zero: ok=%v v=%v", s.RSSOK, s.RSSMiB)
	}
	if !s.MemAvailOK || s.MemAvailableMiB <= 0 {
		t.Fatalf("MemAvailable unavailable or zero: ok=%v v=%v", s.MemAvailOK, s.MemAvailableMiB)
	}
	if s.SystemCPU < 0 || s.SystemCPU > 100.01 {
		t.Fatalf("implausible system CPU%%: %v", s.SystemCPU)
	}
	if s.GCPauseMs < 0 {
		t.Fatalf("negative GC pause: %v", s.GCPauseMs)
	}
}

func TestSamplesSince(t *testing.T) {
	m := New(time.Second, 10)
	base := time.Now()
	m.mu.Lock()
	m.samples = []Sample{{Time: base}, {Time: base.Add(time.Second)}, {Time: base.Add(2 * time.Second)}}
	m.mu.Unlock()
	got := m.SamplesSince(base)
	if len(got) != 2 {
		t.Fatalf("got %d samples; want 2", len(got))
	}
	got = m.SamplesSince(base.Add(time.Hour))
	if len(got) != 0 {
		t.Fatalf("got %d samples; want 0", len(got))
	}
}

func TestIsWholeDisk(t *testing.T) {
	cases := map[string]bool{
		"sda": true, "sda1": false, "vda": true, "vda2": false,
		"xvdb": true, "xvdb1": false, "nvme0n1": true, "nvme0n1p2": false,
		"mmcblk0": true, "mmcblk0p1": false, "loop0": false, "dm-0": false,
		"ram1": false, "zram0": false,
	}
	for name, want := range cases {
		if got := isWholeDisk(name); got != want {
			t.Errorf("isWholeDisk(%q) = %v; want %v", name, got, want)
		}
	}
}
