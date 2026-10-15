package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"memory-bound/internal/monitor"
	"memory-bound/internal/store"
)

func newTestServer(t *testing.T, cfg Config) (*httptest.Server, *store.Store, *monitor.Monitor) {
	t.Helper()
	st := store.New()
	mon := monitor.New(50*time.Millisecond, 100)
	mon.Start()
	t.Cleanup(mon.Stop)
	s := New(st, cfg, mon)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, st, mon
}

func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp, buf.Bytes()
}

func TestHealth(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{})
	resp, body := get(t, srv.URL+"/health")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Fatalf("body = %s", body)
	}
}

func TestPutGetMissing(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{})
	resp, _ := get(t, srv.URL+"/kv/missing")
	if resp.StatusCode != 404 {
		t.Fatalf("missing key status = %d; want 404", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/kv/alpha", strings.NewReader("payload"))
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 204 {
		t.Fatalf("put status = %d; want 204", resp2.StatusCode)
	}
	resp3, body := get(t, srv.URL+"/kv/alpha")
	if resp3.StatusCode != 200 || string(body) != "payload" {
		t.Fatalf("get = %d %q", resp3.StatusCode, body)
	}
}

func TestStatsCounters(t *testing.T) {
	srv, st, _ := newTestServer(t, Config{})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/kv/a", strings.NewReader("12345678"))
	http.DefaultClient.Do(req)
	for i := 0; i < 3; i++ {
		get(t, srv.URL+"/kv/a")
	}
	get(t, srv.URL+"/kv/nope") // 404, not counted
	resp, body := get(t, srv.URL+"/stats")
	if resp.StatusCode != 200 {
		t.Fatalf("stats status = %d", resp.StatusCode)
	}
	var out struct {
		Entries      int    `json:"entries"`
		TotalGets    uint64 `json:"total_gets"`
		TotalPuts    uint64 `json:"total_puts"`
		LogicalBytes int64  `json:"logical_bytes"`
		ValueSize    int    `json:"value_size"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Entries != 1 || out.TotalGets != 3 || out.TotalPuts != 1 {
		t.Fatalf("stats = %+v", out)
	}
	if out.LogicalBytes != 8 {
		t.Fatalf("logical bytes = %d; want 8", out.LogicalBytes)
	}
	_ = st
}

func TestPopulateAndRandomAndBatch(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{Admin: true, MaxBatch: 16})

	pop := `{"entries":100,"value_size":32}`
	resp, err := http.Post(srv.URL+"/admin/populate", "application/json", strings.NewReader(pop))
	if err != nil {
		t.Fatal(err)
	}
	var pr populateResponse
	json.NewDecoder(resp.Body).Decode(&pr)
	resp.Body.Close()
	if resp.StatusCode != 200 || pr.Entries != 100 || pr.LogicalBytes != 3200 {
		t.Fatalf("populate = %d %+v", resp.StatusCode, pr)
	}

	// Deterministic key content.
	_, body := get(t, srv.URL+"/kv/"+store.Key(7))
	if len(body) != 32 || binary.BigEndian.Uint32(body) != 7 {
		t.Fatalf("value body = %d bytes, idx %d", len(body), binary.BigEndian.Uint32(body))
	}

	// Random returns 200 with a valid value.
	resp2, rbody := get(t, srv.URL+"/random")
	if resp2.StatusCode != 200 || len(rbody) != 32 {
		t.Fatalf("random = %d len %d", resp2.StatusCode, len(rbody))
	}

	// Batch format: n frames of [len][value].
	resp3, bbody := get(t, srv.URL+"/batch?n=5")
	if resp3.StatusCode != 200 {
		t.Fatalf("batch status = %d", resp3.StatusCode)
	}
	if len(bbody) != 5*(4+32) {
		t.Fatalf("batch body len = %d", len(bbody))
	}
	for off := 0; off < len(bbody); off += 36 {
		if l := binary.BigEndian.Uint32(bbody[off : off+4]); l != 32 {
			t.Fatalf("frame length = %d", l)
		}
	}
}

func TestBatchInvalidParams(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{Admin: true, MaxBatch: 8})
	pop := `{"entries":50,"value_size":8}`
	http.Post(srv.URL+"/admin/populate", "application/json", strings.NewReader(pop))

	for _, q := range []string{"", "?n=0", "?n=-3", "?n=9", "?n=abc", "?n=1000"} {
		resp, body := get(t, srv.URL+"/batch"+q)
		if resp.StatusCode != 400 {
			t.Fatalf("batch %q status = %d (%s); want 400", q, resp.StatusCode, body)
		}
	}
	// n larger than dataset but within max: returns fewer frames, not an error.
	resp, body := get(t, srv.URL+"/batch?n=8")
	if resp.StatusCode != 200 || len(body) != 8*(4+8) {
		t.Fatalf("batch clamp: %d len=%d", resp.StatusCode, len(body))
	}
}

func TestBatchOnEmptyStore(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{MaxBatch: 8})
	resp, _ := get(t, srv.URL+"/batch?n=2")
	if resp.StatusCode != 404 {
		t.Fatalf("empty batch status = %d; want 404", resp.StatusCode)
	}
}

func TestPutBodyTooLarge(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{MaxBodyBytes: 16})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/kv/big", strings.NewReader(strings.Repeat("x", 64)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("status = %d; want 413", resp.StatusCode)
	}
}

func TestAdminGating(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{Admin: false})
	resp, _ := get(t, srv.URL+"/admin/metrics")
	if resp.StatusCode != 404 {
		t.Fatalf("admin metrics with admin off = %d; want 404", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/populate", strings.NewReader("{}"))
	r2, _ := http.DefaultClient.Do(req)
	r2.Body.Close()
	if r2.StatusCode != 404 {
		t.Fatalf("populate with admin off = %d; want 404", r2.StatusCode)
	}
}

func TestPopulateInvalidBodies(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{Admin: true})
	for _, body := range []string{"", "{not json", `{"entries":-5,"value_size":8}`, `{"entries":10,"value_size":999999999}`} {
		resp, err := http.Post(srv.URL+"/admin/populate", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("populate %q status = %d; want 400", body, resp.StatusCode)
		}
	}
}

func TestConcurrentPopulationRejected(t *testing.T) {
	srv, st, _ := newTestServer(t, Config{Admin: true})
	// Occupy the store's populate lock with a slow population of many entries.
	started := make(chan struct{})
	go func() {
		close(started)
		st.Populate(200_000, 128)
	}()
	<-started
	// Give the first populate a moment to take the lock.
	time.Sleep(10 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(srv.URL+"/admin/populate", "application/json", strings.NewReader(`{"entries":10,"value_size":8}`))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 409 {
				t.Errorf("concurrent populate status = %d; want 409", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
}

func TestAdminMetricsEndpoint(t *testing.T) {
	srv, _, mon := newTestServer(t, Config{Admin: true})
	// Take samples manually and wait for the loop to gather a few.
	time.Sleep(150 * time.Millisecond)
	since := mon.StartWall().UnixNano()
	resp, err := http.Get(fmt.Sprintf("%s/admin/metrics?since=%d", srv.URL, since))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		IntervalMs int              `json:"interval_ms"`
		Samples    []map[string]any `json:"samples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Samples) == 0 {
		t.Fatal("no samples returned")
	}
	if _, ok := payload.Samples[0]["unix_nano"]; !ok {
		t.Fatalf("sample missing unix_nano: %v", payload.Samples[0])
	}
	// Invalid since must be rejected.
	r2, _ := http.Get(srv.URL + "/admin/metrics?since=abc")
	r2.Body.Close()
	if r2.StatusCode != 400 {
		t.Fatalf("invalid since = %d", r2.StatusCode)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, _, _ := newTestServer(t, Config{})
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/kv/a", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405", resp.StatusCode)
	}
}

func TestMemoryBudgetEnforced(t *testing.T) {
	srv, st, _ := newTestServer(t, Config{Admin: true, MemLimitPct: 80})
	st.SetBudget(100)

	// Populate within budget succeeds.
	resp, err := http.Post(srv.URL+"/admin/populate", "application/json",
		strings.NewReader(`{"entries":10,"value_size":10}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("populate within budget = %d", resp.StatusCode)
	}

	// Populate beyond budget: 507, dataset unchanged.
	resp, err = http.Post(srv.URL+"/admin/populate", "application/json",
		strings.NewReader(`{"entries":50,"value_size":10}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("populate over budget = %d (%s); want 507", resp.StatusCode, body)
	}
	if st.Len() != 10 {
		t.Fatalf("entries = %d; want 10 (rejected populate must not modify)", st.Len())
	}

	// PUT beyond budget: 507, value not stored.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/kv/new-key",
		strings.NewReader(strings.Repeat("x", 1)))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	// valueBytes is 100 == budget, so even 1 byte over is rejected.
	if r.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("put over budget = %d; want 507", r.StatusCode)
	}
	if _, ok := st.Get("new-key"); ok {
		t.Fatal("budget-exceeded value stored")
	}

	// /stats reports the budget and limit percentage.
	_, sbody := get(t, srv.URL+"/stats")
	var out struct {
		MemBudgetBytes uint64  `json:"mem_budget_bytes"`
		MemLimitPct    float64 `json:"mem_limit_pct"`
	}
	if err := json.Unmarshal(sbody, &out); err != nil {
		t.Fatal(err)
	}
	if out.MemBudgetBytes != 100 || out.MemLimitPct != 80 {
		t.Fatalf("stats budget = %d / %.0f%%", out.MemBudgetBytes, out.MemLimitPct)
	}
}
