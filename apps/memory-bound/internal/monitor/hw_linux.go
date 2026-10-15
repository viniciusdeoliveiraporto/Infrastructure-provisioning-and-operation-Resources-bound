//go:build linux

package monitor

import (
	"encoding/binary"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// Hardware performance counters (PMU) via perf_event_open(2).
//
// Available on bare metal and most cloud guests (e.g. EC2); usually NOT
// exposed inside VirtualBox guests — there open fails and callers record the
// metrics as unavailable. No value is ever fabricated.
//
// All counters are cumulative; interval rates are derived by the caller.
//
// Go is multi-threaded: a single perf_event_open(pid=TGID) only counts the
// thread-group leader, so net/http work on other threads would be invisible
// (counters appear frozen). We therefore open one counter set per OS thread
// in /proc/self/task and sum them on every read, picking up new threads as
// the runtime creates them.

const (
	perfTypeHardware = 0 // PERF_TYPE_HARDWARE

	perfHWCPUCycles    = 0 // PERF_COUNT_HW_CPU_CYCLES
	perfHWInstructions = 1 // PERF_COUNT_HW_INSTRUCTIONS
	perfHWCacheRefs    = 2 // PERF_COUNT_HW_CACHE_REFERENCES
	perfHWCacheMisses  = 3 // PERF_COUNT_HW_CACHE_MISSES

	perfFlagExcludeKernel = 1 << 5
	perfFlagExcludeHV     = 1 << 6
)

// perfEventAttr mirrors struct perf_event_attr (first 72 bytes = VER1).
// Field order and sizes must match the kernel ABI.
type perfEventAttr struct {
	Type         uint32
	Size         uint32
	Config       uint64
	SamplePeriod uint64
	SampleType   uint64
	ReadFormat   uint64
	Flags        uint64
	WakeupEvents uint32
	BPType       uint32
	Config1      uint64
	Config2      uint64
}

type threadCounters struct {
	tid     int
	cycles  int
	inst    int
	refs    int
	misses  int
}

// hwCounters holds open perf fds for every OS thread of this process.
type hwCounters struct {
	threads []threadCounters
	flags   uint64
	ok      bool
}

// openHWCounters opens PMU counters for the calling thread, then expands to
// all threads via refresh. Falls back to user-only counting when
// perf_event_paranoid forbids kernel counts. Returns nil when no thread can
// be instrumented (typical inside VirtualBox guests).
func openHWCounters() *hwCounters {
	flags := uint64(0)
	if !canOpenPerf(flags) {
		flags = perfFlagExcludeKernel | perfFlagExcludeHV
		if !canOpenPerf(flags) {
			return nil
		}
	}
	c := &hwCounters{flags: flags}
	c.refresh()
	if !c.ok {
		return nil
	}
	return c
}

func canOpenPerf(flags uint64) bool {
	fd := perfOpen(perfTypeHardware, perfHWCPUCycles, flags, syscall.Gettid())
	if fd < 0 {
		return false
	}
	syscall.Close(fd)
	return true
}

// refresh syncs the open counter set with the current /proc/self/task list.
// New threads get counters; exited threads are closed.
func (c *hwCounters) refresh() {
	tids, err := listThreads()
	if err != nil {
		return
	}
	alive := make(map[int]bool, len(tids))
	for _, tid := range tids {
		alive[tid] = true
	}
	// Drop dead threads.
	kept := c.threads[:0]
	for _, t := range c.threads {
		if alive[t.tid] {
			kept = append(kept, t)
			delete(alive, t.tid)
		} else {
			closeThreadCounters(t)
		}
	}
	c.threads = kept
	// Add new threads.
	for tid := range alive {
		t := openThreadCounters(tid, c.flags)
		if t.tid != 0 {
			c.threads = append(c.threads, t)
		}
	}
	c.ok = len(c.threads) > 0
}

func openThreadCounters(tid int, flags uint64) threadCounters {
	t := threadCounters{tid: tid, cycles: -1, inst: -1, refs: -1, misses: -1}
	t.cycles = perfOpen(perfTypeHardware, perfHWCPUCycles, flags, tid)
	t.inst = perfOpen(perfTypeHardware, perfHWInstructions, flags, tid)
	t.refs = perfOpen(perfTypeHardware, perfHWCacheRefs, flags, tid)
	t.misses = perfOpen(perfTypeHardware, perfHWCacheMisses, flags, tid)
	if t.cycles < 0 || t.inst < 0 || t.refs < 0 || t.misses < 0 {
		closeThreadCounters(t)
		return threadCounters{}
	}
	return t
}

func closeThreadCounters(t threadCounters) {
	for _, fd := range []int{t.cycles, t.inst, t.refs, t.misses} {
		if fd >= 0 {
			syscall.Close(fd)
		}
	}
}

func listThreads() ([]int, error) {
	ents, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return nil, err
	}
	tids := make([]int, 0, len(ents))
	for _, e := range ents {
		if tid, err := strconv.Atoi(e.Name()); err == nil {
			tids = append(tids, tid)
		}
	}
	return tids, nil
}

func perfOpen(typ, config, flags uint64, tid int) int {
	attr := perfEventAttr{
		Type:   uint32(typ),
		Size:   uint32(unsafe.Sizeof(perfEventAttr{})),
		Config: config,
		Flags:  flags,
	}
	// tid > 0, cpu = -1: count this specific OS thread on any CPU.
	r1, _, errno := syscall.Syscall6(
		syscall.SYS_PERF_EVENT_OPEN,
		uintptr(unsafe.Pointer(&attr)),
		uintptr(tid),
		^uintptr(0), // cpu: -1 (all CPUs)
		^uintptr(0), // group_fd: -1 (no group)
		0,           // flags
		0,
	)
	if errno != 0 {
		return -1
	}
	return int(r1)
}

// read sums cumulative counters across all instrumented threads. ok is false
// when nothing can be read (VM without counter passthrough, or all threads
// failed). Callers should refresh at a low frequency to pick up new threads.
func (c *hwCounters) read() (cycles, inst, refs, misses uint64, ok bool) {
	if c == nil || !c.ok {
		return 0, 0, 0, 0, false
	}
	// Cheap rescan so short-lived runtime threads are not missed. Opening
	// events is rare after warmup (thread count is stable).
	c.refresh()
	if len(c.threads) == 0 {
		return 0, 0, 0, 0, false
	}
	var allOK = true
	for _, t := range c.threads {
		v, ok1 := perfRead(t.cycles)
		i, ok2 := perfRead(t.inst)
		r, ok3 := perfRead(t.refs)
		m, ok4 := perfRead(t.misses)
		if !(ok1 && ok2 && ok3 && ok4) {
			allOK = false
			continue
		}
		cycles += v
		inst += i
		refs += r
		misses += m
	}
	return cycles, inst, refs, misses, allOK && len(c.threads) > 0
}

func perfRead(fd int) (uint64, bool) {
	if fd < 0 {
		return 0, false
	}
	var buf [8]byte
	n, err := syscall.Read(fd, buf[:])
	if err != nil || n != 8 {
		return 0, false
	}
	return binary.LittleEndian.Uint64(buf[:]), true
}

func (c *hwCounters) close() {
	if c == nil {
		return
	}
	for _, t := range c.threads {
		closeThreadCounters(t)
	}
	c.threads = nil
	c.ok = false
}

// pageSizeBytes returns the system page size for vmstat conversions.
func pageSizeBytes() uint64 { return uint64(os.Getpagesize()) }
