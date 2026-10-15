package benchmark

import (
	"testing"
	"time"
)

func baseConfig() Config {
	return Config{
		URL:            "http://127.0.0.1:8080",
		Mode:           ModeRandomRead,
		Duration:       10 * time.Second,
		Warmup:         time.Second,
		Concurrency:    4,
		ReadRatio:      100,
		ValueSize:      64,
		DatasetEntries: 100,
		BatchSize:      8,
		Seed:           42,
		HotsetFraction: 0.1,
	}
}

func TestResolveValid(t *testing.T) {
	c := baseConfig()
	if err := c.Resolve(1000); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.DatasetEntries != 100 {
		t.Fatalf("explicit dataset entries overwritten: %d", c.DatasetEntries)
	}
	if c.ResultsDir != "results" {
		t.Fatalf("ResultsDir = %q", c.ResultsDir)
	}
}

func TestResolveFillsDatasetFromServer(t *testing.T) {
	c := baseConfig()
	c.DatasetEntries = 0
	if err := c.Resolve(5000); err != nil {
		t.Fatal(err)
	}
	if c.DatasetEntries != 5000 {
		t.Fatalf("DatasetEntries = %d; want 5000", c.DatasetEntries)
	}
}

func TestInvalidWorkloadConfigurations(t *testing.T) {
	cases := []struct {
		name          string
		serverEntries int
		mutate        func(*Config)
	}{
		{"empty url", 100, func(c *Config) { c.URL = "" }},
		{"unknown mode", 100, func(c *Config) { c.Mode = "zipf" }},
		{"zero duration", 100, func(c *Config) { c.Duration = 0 }},
		{"negative warmup", 100, func(c *Config) { c.Warmup = -time.Second }},
		{"zero concurrency", 100, func(c *Config) { c.Concurrency = 0 }},
		{"read ratio too high", 100, func(c *Config) { c.ReadRatio = 101 }},
		{"read ratio negative", 100, func(c *Config) { c.ReadRatio = -1 }},
		{"negative value size", 100, func(c *Config) { c.ValueSize = -1 }},
		{"negative rate", 100, func(c *Config) { c.Rate = -5 }},
		{"batch without batch size", 100, func(c *Config) { c.Mode = ModeBatchRead; c.BatchSize = 0 }},
		{"hotset fraction out of range", 100, func(c *Config) { c.Mode = ModeHotset; c.HotsetFraction = 1.5 }},
		{"no entries and server empty", 0, func(c *Config) { c.DatasetEntries = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := baseConfig()
			tc.mutate(&c)
			if err := c.Resolve(tc.serverEntries); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestHotsetSize(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeHotset
	c.DatasetEntries = 10_000
	c.HotsetFraction = 0.01
	if err := c.Resolve(0); err != nil {
		t.Fatal(err)
	}
	if got := HotsetSize(&c); got != 100 {
		t.Fatalf("HotsetSize = %d; want 100", got)
	}
	c.HotsetFraction = 0.5
	if got := HotsetSize(&c); got != 5000 {
		t.Fatalf("HotsetSize = %d; want 5000", got)
	}
}

func TestEffectiveReadRatio(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeReadWrite
	c.ReadRatio = 70
	if got := c.EffectiveReadRatio(); got != 70 {
		t.Fatalf("read-write ratio = %d; want 70", got)
	}
	c.Mode = ModeBatchRead
	if got := c.EffectiveReadRatio(); got != 100 {
		t.Fatalf("batch ratio = %d; want 100", got)
	}
}
