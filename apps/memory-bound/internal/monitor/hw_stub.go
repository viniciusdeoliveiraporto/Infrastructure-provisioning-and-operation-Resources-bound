//go:build !linux

package monitor

// Stubs for non-Linux platforms: hardware counters and /proc derived
// metrics are simply unavailable.

type hwCounters struct{}

func openHWCounters() *hwCounters { return nil }

func (c *hwCounters) read() (cycles, inst, refs, misses uint64, ok bool) {
	return 0, 0, 0, 0, false
}

func (c *hwCounters) close() {}

func pageSizeBytes() uint64 { return 4096 }
