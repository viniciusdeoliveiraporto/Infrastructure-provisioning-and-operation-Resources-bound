package monitor

import (
	"os"
	"strconv"
	"strings"
)

// The functions below read cumulative counters from Linux /proc. On any
// error (missing file, unexpected format, non-Linux platform) they return
// ok=false; callers mark the derived metric unavailable.

// procSelfCPUTicks returns utime+stime of the current process in clock ticks.
func procSelfCPUTicks() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// Fields after the comm (which is parenthesized and may contain spaces).
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	rest := strings.Fields(s[i+2:])
	// After comm: state=field3, so utime is field 14 -> rest index 11,
	// stime field 15 -> rest index 12.
	if len(rest) < 13 {
		return 0, false
	}
	ut, err1 := strconv.ParseUint(rest[11], 10, 64)
	st, err2 := strconv.ParseUint(rest[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return ut + st, true
}

// procSystemCPU returns busy and total jiffies across all CPUs from /proc/stat.
func procSystemCPU() (busy, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line := firstLine(string(b))
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	var vals [10]uint64
	for i := 1; i < len(f) && i <= 10; i++ {
		v, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		vals[i-1] = v
		total += v
	}
	// idle is field 4 (index 3), iowait field 5 (index 4).
	idle := vals[3] + vals[4]
	if total < idle {
		return 0, 0, false
	}
	return total - idle, total, true
}

// procSelfRSS returns the resident set size of this process in MiB.
func procSelfRSS() (mib float64, ok bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return 0, false
		}
		kb, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			return 0, false
		}
		return kb / 1024, true
	}
	return 0, false
}

// AvailableMemoryBytes reports MemAvailable from /proc/meminfo — the
// approximate RAM available for new workloads without swapping. ok is false
// when it cannot be read (non-Linux, unusual kernels).
func AvailableMemoryBytes() (uint64, bool) {
	mib, ok := procMemAvailable()
	if !ok {
		return 0, false
	}
	return uint64(mib * (1 << 20)), true
}

// procMemAvailable returns MemAvailable from /proc/meminfo in MiB.
func procMemAvailable() (mib float64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			return 0, false
		}
		return kb / 1024, true
	}
	return 0, false
}

// procDiskSectors returns cumulative read/written sectors for whole-disk
// devices. Partitions (sda1, nvme0n1p1, ...) are skipped to avoid double
// counting; loop and ram devices are skipped as well.
func procDiskSectors() (read, written uint64, ok bool) {
	b, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return 0, 0, false
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		name := f[2]
		if !isWholeDisk(name) {
			continue
		}
		r, err1 := strconv.ParseUint(f[5], 10, 64)
		w, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		read += r
		written += w
		found = true
	}
	return read, written, found
}

func isWholeDisk(name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
		strings.HasPrefix(name, "fd") || strings.HasPrefix(name, "sr") ||
		strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "zram") ||
		strings.HasPrefix(name, "md") {
		return false
	}
	// Find the trailing digit run: none => whole disk (sda, vda, nvme0n1).
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	if i == len(name) {
		return true
	}
	if i == 0 {
		return false
	}
	// nvme0n1p2 / mmcblk0p1: 'p' before digits means partition.
	if name[i-1] == 'p' && (strings.HasPrefix(name, "nvme") || strings.HasPrefix(name, "mmcblk")) {
		return false
	}
	// nvme0n1 / mmcblk0: digits not preceded by 'p' => whole disk.
	if strings.HasPrefix(name, "nvme") || strings.HasPrefix(name, "mmcblk") {
		return true
	}
	// sda1, vda2, xvdb3: letters followed by digits => partition.
	return false
}

// procNetBytes returns cumulative receive/transmit bytes excluding loopback.
func procNetBytes() (rx, tx uint64, ok bool) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(string(b), "\n")
	found := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "lo" || strings.HasPrefix(name, "face") {
			continue // skip header line too ("Inter-| face")
		}
		f := strings.Fields(line[colon+1:])
		if len(f) < 16 {
			continue
		}
		r, err1 := strconv.ParseUint(f[0], 10, 64)
		t, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += r
		tx += t
		found = true
	}
	return rx, tx, found
}

// procSelfFaults returns cumulative minor/major page faults of this process.
func procSelfFaults() (minflt, majflt uint64, ok bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, 0, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, 0, false
	}
	rest := strings.Fields(s[i+2:])
	// After comm: state=field3 => minflt(10)=rest[7], majflt(12)=rest[9].
	if len(rest) < 13 {
		return 0, 0, false
	}
	mn, e1 := strconv.ParseUint(rest[7], 10, 64)
	mj, e2 := strconv.ParseUint(rest[9], 10, 64)
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	return mn, mj, true
}

// procVMStatSwap returns cumulative system swap-in/swap-out pages from
// /proc/vmstat (kernel 2.6+; present inside VMs as well).
func procVMStatSwap() (in, out uint64, ok bool) {
	b, err := os.ReadFile("/proc/vmstat")
	if err != nil {
		return 0, 0, false
	}
	var gotIn, gotOut bool
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "pswpin":
			in, gotIn = v, true
		case "pswpout":
			out, gotOut = v, true
		}
		if gotIn && gotOut {
			return in, out, true
		}
	}
	return 0, 0, false
}

// procPSI reads the "some" avg10 pressure values for CPU and memory.
func procPSI() (cpuSome, memSome float64, ok bool) {
	c, cok := readPSIAvg10("/proc/pressure/cpu")
	m, mok := readPSIAvg10("/proc/pressure/memory")
	return c, m, cok && mok
}

func readPSIAvg10(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if !strings.HasPrefix(f, "avg10=") {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimPrefix(f, "avg10="), 64)
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
