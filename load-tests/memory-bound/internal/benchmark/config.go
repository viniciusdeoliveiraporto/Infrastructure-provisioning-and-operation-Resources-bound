// Package benchmark implements the configurable workload generator and
// benchmark result collection for the in-memory key-value server.
package benchmark

import (
	"fmt"
	"strings"
	"time"
)

// Workload modes.
const (
	ModeRandomRead     = "random-read"
	ModeReadWrite      = "read-write"
	ModeSequentialRead = "sequential-read"
	ModeBatchRead      = "batch-read"
	ModeHotset         = "hotset"
)

// AllModes lists the supported workload modes.
func AllModes() []string {
	return []string{ModeRandomRead, ModeReadWrite, ModeSequentialRead, ModeBatchRead, ModeHotset}
}

// Config is the effective workload configuration. The seed and the whole
// resolved configuration are recorded in summary.json for reproducibility.
type Config struct {
	URL            string
	Mode           string
	Duration       time.Duration
	Warmup         time.Duration
	Concurrency    int
	ReadRatio      int // percent of reads for read-write mode
	ValueSize      int // write payload bytes
	DatasetEntries int // 0 = resolve from server /stats
	BatchSize      int // batch-read mode
	Seed           int64
	Rate           float64 // target HTTP req/s; 0 = unlimited
	HotsetFraction float64 // fraction of the dataset in the hot set (hotset mode)

	Label      string // free-form infrastructure label (e.g. "m5.large")
	ResultsDir string
	PlotScript string // empty disables automatic plotting
	NoPopulate bool   // skip the pre-benchmark POST /admin/populate
}

// Resolve fills defaults and validates the configuration. datasetEntries
// may be filled from serverEntries (fetched from /stats) when unset.
func (c *Config) Resolve(serverEntries int) error {
	if c.URL == "" {
		return fmt.Errorf("-url is required")
	}
	c.URL = strings.TrimRight(c.URL, "/")
	if c.Mode == "" {
		return fmt.Errorf("-mode is required (one of %s)", strings.Join(AllModes(), ", "))
	}
	valid := false
	for _, m := range AllModes() {
		if c.Mode == m {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid -mode %q (one of %s)", c.Mode, strings.Join(AllModes(), ", "))
	}
	if c.Duration <= 0 {
		return fmt.Errorf("-duration must be > 0")
	}
	if c.Warmup < 0 {
		return fmt.Errorf("-warmup must be >= 0")
	}
	if c.Concurrency < 1 {
		return fmt.Errorf("-concurrency must be >= 1")
	}
	if c.ReadRatio < 0 || c.ReadRatio > 100 {
		return fmt.Errorf("-read-ratio must be in [0,100]")
	}
	if c.ValueSize < 0 {
		return fmt.Errorf("-value-size must be >= 0")
	}
	if c.BatchSize < 0 {
		return fmt.Errorf("-batch-size must be >= 0")
	}
	if c.Rate < 0 {
		return fmt.Errorf("-rate must be >= 0")
	}
	if c.HotsetFraction == 0 {
		c.HotsetFraction = 0.01
	}
	if c.HotsetFraction <= 0 || c.HotsetFraction > 1 {
		return fmt.Errorf("-hotset-fraction must be in (0,1]")
	}
	if c.DatasetEntries == 0 {
		c.DatasetEntries = serverEntries
	}
	if c.DatasetEntries <= 0 {
		return fmt.Errorf("-dataset-entries must be > 0 (server reported %d entries; populate the dataset first)", serverEntries)
	}

	switch c.Mode {
	case ModeBatchRead:
		if c.BatchSize < 1 {
			return fmt.Errorf("-batch-size must be >= 1 in %s mode", ModeBatchRead)
		}
	case ModeReadWrite:
		// ReadRatio already validated; nothing extra.
	case ModeHotset:
		if hotsetSize(c) < 1 {
			return fmt.Errorf("hot set is empty: -hotset-fraction %.4f of %d entries", c.HotsetFraction, c.DatasetEntries)
		}
	}
	if c.ResultsDir == "" {
		c.ResultsDir = "results"
	}
	return nil
}

// HotsetSize returns the number of entries in the hot set for hotset mode.
func HotsetSize(c *Config) int {
	return hotsetSize(c)
}

func hotsetSize(c *Config) int {
	n := int(float64(c.DatasetEntries) * c.HotsetFraction)
	if n < 1 {
		return 1
	}
	if n > c.DatasetEntries {
		return c.DatasetEntries
	}
	return n
}

// EffectiveReadRatio returns the intended read percentage for the mode.
func (c *Config) EffectiveReadRatio() int {
	switch c.Mode {
	case ModeRandomRead, ModeSequentialRead, ModeBatchRead:
		return 100
	case ModeReadWrite:
		return c.ReadRatio
	case ModeHotset:
		return 100
	}
	return 0
}
