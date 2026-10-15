package benchmark

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientSample is one client-side resource observation, used to detect load
// generator saturation.
type ClientSample struct {
	UnixNano int64
	CPUPct   float64
	CPUOK    bool
	RSSMiB   float64
	RSSOK    bool
}

// clientMonitor samples client process CPU and RSS about once per second.
type clientMonitor struct {
	mu      sync.Mutex
	samples []ClientSample

	prevTicks uint64
	prevTime  time.Time
	hasPrev   bool

	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func newClientMonitor() *clientMonitor {
	return &clientMonitor{
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (m *clientMonitor) start() {
	go func() {
		defer close(m.stopped)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-m.stop:
				return
			case now := <-t.C:
				s := m.sampleOnce(now)
				m.mu.Lock()
				m.samples = append(m.samples, s)
				m.mu.Unlock()
			}
		}
	}()
}

func (m *clientMonitor) stopSampling() {
	m.once.Do(func() { close(m.stop) })
	<-m.stopped
}

func (m *clientMonitor) sampleOnce(now time.Time) ClientSample {
	s := ClientSample{UnixNano: now.UnixNano()}
	if kb, ok := procStatusKB("VmRSS"); ok {
		s.RSSMiB = kb / 1024
		s.RSSOK = true
	}
	ticks, ok := procSelfCPUTicks()
	if ok {
		m.mu.Lock()
		prev, hasPrev, prevTime := m.prevTicks, m.hasPrev, m.prevTime
		m.prevTicks, m.hasPrev, m.prevTime = ticks, true, now
		m.mu.Unlock()
		if hasPrev && now.After(prevTime) {
			wall := now.Sub(prevTime).Seconds()
			s.CPUPct = float64(ticks-prev) / 100.0 / wall * 100
			s.CPUOK = true
		}
	}
	return s
}

func (m *clientMonitor) snapshot() []ClientSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ClientSample, len(m.samples))
	copy(out, m.samples)
	return out
}

// procSelfCPUTicks reads utime+stime of this process from /proc/self/stat
// (Linux only; returns ok=false elsewhere).
func procSelfCPUTicks() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	rest := strings.Fields(s[i+2:])
	if len(rest) < 13 {
		return 0, false
	}
	ut, e1 := strconv.ParseUint(rest[11], 10, 64)
	st, e2 := strconv.ParseUint(rest[12], 10, 64)
	if e1 != nil || e2 != nil {
		return 0, false
	}
	return ut + st, true
}

func procStatusKB(field string) (float64, bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	prefix := field + ":"
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0, false
		}
		v, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}
