// Package monitor collects timestamped resource metrics for the server
// process and the whole system. Sampling runs on a background goroutine,
// completely independent of request handlers. Samples are buffered in
// memory and served to the load generator over HTTP after a benchmark run.
//
// All rates are computed from cumulative counter deltas between consecutive
// samples. Metrics that cannot be read (missing /proc files, non-Linux
// platforms) are marked unavailable rather than fabricated.
package monitor

import (
	"runtime"
	"sync"
	"time"
)

// clockTicks is the kernel USER_HZ used by /proc/[pid]/stat. Almost every
// Linux distribution uses 100 on x86_64/arm64.
const clockTicks = 100.0

// Sample is one timestamped resource observation. Fields with OK flags hold
// meaningful data only when the flag is true.
type Sample struct {
	Time time.Time

	CPUPct    float64 // process CPU, percent of a single core (may exceed 100)
	CPUOK     bool
	SystemCPU float64 // whole-system CPU utilization, percent (0-100)
	SystemOK  bool

	RSSMiB  float64 // process resident set size
	RSSOK   bool
	HeapMiB float64 // Go heap allocated bytes (runtime metric, all platforms)
	HeapOK  bool

	MemAvailableMiB float64 // system-wide MemAvailable
	MemAvailOK      bool

	GCCount   uint64  // cumulative GC cycles
	GCPauseMs float64 // GC pause time accumulated during this interval
	GCOK      bool

	DiskReadMiBs  float64 // whole-system disk read throughput
	DiskWriteMiBs float64
	DiskOK        bool

	NetRxMiBs float64 // whole-system network receive throughput (lo excluded)
	NetTxMiBs float64
	NetOK     bool

	PSICPU float64 // /proc/pressure/cpu "some" avg10
	PSIMem float64 // /proc/pressure/memory "some" avg10
	PSIOK  bool

	PageFaultsPerS  float64 // process minor faults per second
	MajorFaultsPerS float64 // process major (swap-in) faults per second
	FaultsOK        bool

	SwapInMiBs  float64 // system-wide swap in throughput (/proc/vmstat pswpin)
	SwapOutMiBs float64 // system-wide swap out throughput
	SwapOK      bool

	// Hardware performance counters (PMU); unavailable inside most VMs.
	IPC             float64 // instructions / cycles over the interval
	CacheMissesPerS float64
	CacheMissRate   float64 // cache misses / cache references over the interval
	HWOK            bool

	// Cumulative baselines used to compute deltas on the next sample.
	cpuTicks    uint64 // utime+stime of this process
	minFlt      uint64 // process minor faults
	majFlt      uint64 // process major faults
	sysBusy     uint64
	sysTotal    uint64
	diskReadSec uint64
	diskWritSec uint64
	netRx       uint64
	netTx       uint64
	pswpin      uint64 // system swap-in pages
	pswpout     uint64 // system swap-out pages
	pauseTotal  uint64
	cycles      uint64
	insts       uint64
	cacheRefs   uint64
	cacheMisses uint64
}

// Monitor periodically samples resource metrics into a bounded buffer.
type Monitor struct {
	interval time.Duration
	max      int

	start time.Time

	mu      sync.Mutex
	samples []Sample
	prev    Sample
	hasPrev bool

	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once

	hw *hwCounters // PMU counters, nil when unsupported
}

// New creates a monitor that samples approximately every interval and keeps
// at most max samples (the oldest are dropped).
func New(interval time.Duration, max int) *Monitor {
	if interval <= 0 {
		interval = time.Second
	}
	if max <= 0 {
		max = 7200
	}
	return &Monitor{
		interval: interval,
		max:      max,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Start begins sampling in the background.
func (m *Monitor) Start() {
	m.start = time.Now()
	m.hw = openHWCounters()
	go m.loop()
}

// Stop terminates the sampling loop and waits for it to finish.
func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.stop) })
	<-m.stopped
	m.hw.close()
}

func (m *Monitor) loop() {
	defer close(m.stopped)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			s := m.SampleOnce(now)
			m.mu.Lock()
			m.samples = append(m.samples, s)
			if len(m.samples) > m.max {
				m.samples = m.samples[len(m.samples)-m.max:]
			}
			m.mu.Unlock()
		}
	}
}

// SampleOnce takes a single sample, computes interval rates from the
// previous sample, and becomes the new delta baseline. It is exported so
// tests can exercise the delta logic deterministically.
func (m *Monitor) SampleOnce(now time.Time) Sample {
	s := Sample{Time: now}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.HeapMiB = float64(ms.HeapAlloc) / (1 << 20)
	s.HeapOK = true
	s.GCCount = uint64(ms.NumGC)
	s.pauseTotal = ms.PauseTotalNs

	m.mu.Lock()
	prev := m.prev
	hasPrev := m.hasPrev
	m.mu.Unlock()

	// Process and system cumulative counters (Linux only).
	s.cpuTicks, s.CPUOK = procSelfCPUTicks()
	s.sysBusy, s.sysTotal, s.SystemOK = procSystemCPU()
	s.RSSMiB, s.RSSOK = procSelfRSS()
	s.MemAvailableMiB, s.MemAvailOK = procMemAvailable()
	s.diskReadSec, s.diskWritSec, s.DiskOK = procDiskSectors()
	s.netRx, s.netTx, s.NetOK = procNetBytes()
	s.PSICPU, s.PSIMem, s.PSIOK = procPSI()
	s.minFlt, s.majFlt, s.FaultsOK = procSelfFaults()
	s.pswpin, s.pswpout, s.SwapOK = procVMStatSwap()
	s.cycles, s.insts, s.cacheRefs, s.cacheMisses, s.HWOK = m.hw.read()

	// Install the new baseline (raw counters are needed next sample).
	m.mu.Lock()
	m.prev = s
	m.hasPrev = true
	m.mu.Unlock()

	s.GCOK = true
	if !hasPrev {
		// First sample: no baseline, rates unavailable.
		s.CPUOK = false
		s.SystemOK = false
		s.DiskOK = false
		s.NetOK = false
		s.FaultsOK = false
		s.SwapOK = false
		s.HWOK = false
		s.GCPauseMs = 0
		return s
	}

	wall := now.Sub(prev.Time).Seconds()
	if wall > 0 {
		if s.CPUOK && prev.CPUOK {
			s.CPUPct = float64(s.cpuTicks-prev.cpuTicks) / clockTicks / wall * 100
		} else {
			s.CPUOK = false
		}
		if s.SystemOK && prev.SystemOK {
			total := s.sysTotal - prev.sysTotal
			busy := s.sysBusy - prev.sysBusy
			if total > 0 {
				s.SystemCPU = float64(busy) / float64(total) * 100
			}
		} else {
			s.SystemOK = false
		}
		if s.DiskOK && prev.DiskOK {
			s.DiskReadMiBs = float64(s.diskReadSec-prev.diskReadSec) * 512 / (1 << 20) / wall
			s.DiskWriteMiBs = float64(s.diskWritSec-prev.diskWritSec) * 512 / (1 << 20) / wall
		} else {
			s.DiskOK = false
		}
		if s.NetOK && prev.NetOK {
			s.NetRxMiBs = float64(s.netRx-prev.netRx) / (1 << 20) / wall
			s.NetTxMiBs = float64(s.netTx-prev.netTx) / (1 << 20) / wall
		} else {
			s.NetOK = false
		}
		if s.FaultsOK && prev.FaultsOK {
			s.PageFaultsPerS = float64(s.minFlt-prev.minFlt) / wall
			s.MajorFaultsPerS = float64(s.majFlt-prev.majFlt) / wall
		} else {
			s.FaultsOK = false
		}
		if s.SwapOK && prev.SwapOK {
			ps := float64(pageSizeBytes())
			s.SwapInMiBs = float64(s.pswpin-prev.pswpin) * ps / (1 << 20) / wall
			s.SwapOutMiBs = float64(s.pswpout-prev.pswpout) * ps / (1 << 20) / wall
		} else {
			s.SwapOK = false
		}
		if s.HWOK && prev.HWOK {
			dc := s.cycles - prev.cycles
			di := s.insts - prev.insts
			dm := s.cacheMisses - prev.cacheMisses
			dr := s.cacheRefs - prev.cacheRefs
			if dc == 0 {
				// PMU not advancing (VM without counter passthrough) or
				// idle interval: publish nothing rather than zeros.
				s.HWOK = false
			} else {
				s.IPC = float64(di) / float64(dc)
				s.CacheMissesPerS = float64(dm) / wall
				if dr > 0 {
					s.CacheMissRate = float64(dm) / float64(dr)
				}
			}
		} else {
			s.HWOK = false
		}
	} else {
		s.CPUOK = false
		s.SystemOK = false
		s.DiskOK = false
		s.NetOK = false
		s.FaultsOK = false
		s.SwapOK = false
		s.HWOK = false
	}

	if s.pauseTotal >= prev.pauseTotal {
		s.GCPauseMs = float64(s.pauseTotal-prev.pauseTotal) / 1e6
	} else {
		s.GCPauseMs = 0
	}
	return s
}

// SamplesSince returns copies of samples with timestamps after t.
func (m *Monitor) SamplesSince(t time.Time) []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Sample, 0, len(m.samples))
	for _, s := range m.samples {
		if s.Time.After(t) {
			out = append(out, s)
		}
	}
	return out
}

// StartWall returns the monitor start time.
func (m *Monitor) StartWall() time.Time { return m.start }
