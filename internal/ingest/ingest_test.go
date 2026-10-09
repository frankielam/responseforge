package ingest

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"responseforge/internal/config"
	"responseforge/internal/rule"
	"responseforge/internal/storage"
)

type scriptedLLM struct {
	mu       sync.Mutex
	calls    int
	body     string
	block    chan struct{}
	failOnce error
}

func (s *scriptedLLM) Complete(ctx context.Context, prompt string) (string, error) {
	s.mu.Lock()
	s.calls++
	fail := s.calls == 1 && s.failOnce != nil
	s.mu.Unlock()
	if fail {
		return "", s.failOnce
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.body, nil
}

func (s *scriptedLLM) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestRawFallbackWithoutLLM(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(config.Config{Storage: "csv", MaxBodyBytes: 1024}, store, nil, nil)
	res, err := svc.Ingest(context.Background(), Request{
		URL: "https://example.com/api/items/9", Method: "GET", Status: 200,
		ContentType: "application/json", Body: `{"ok":true}`, CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "raw" || res.LLM != "disabled" || res.Rows != 1 {
		t.Fatalf("%+v", res)
	}
	f, err := os.Open(filepath.Join(dir, "raw.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want header + one raw row, got %d", len(records))
	}
	if records[1][1] != "https://example.com/api/items/9" || records[1][5] != `{"ok":true}` || records[1][7] != "" {
		t.Fatalf("raw row %#v", records[1])
	}
}

func TestLLMGenerateOnceThenParse(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{body: `{
	  "rule": {
	    "pattern": "https://shop.example/api/orders/*",
	    "content_types": ["application/json"],
	    "record_path": "$.data.items[*]",
	    "table": "shop_orders",
	    "fields": [
	      {"name": "order_id", "type": "string", "from": "$.id", "required": true}
	    ]
	  }
	}`}
	svc := New(config.Config{
		Storage: "csv", MaxBodyBytes: 1 << 20, LLMBaseURL: "http://llm.invalid", LLMModel: "mock", LLMTimeout: time.Second, LLMMaxSample: 1000,
	}, store, model, nil)
	body := `{"data":{"items":[{"id":"a"},{"id":"b"}]}}`
	first, err := svc.Ingest(context.Background(), Request{
		URL: "https://shop.example/api/orders/1842?page=3", Method: "GET", Status: 200,
		ContentType: "application/json", Body: body, CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode != "raw" || first.LLM != "scheduled" {
		t.Fatalf("%+v", first)
	}
	waitFor(t, func() bool {
		rules, _ := store.ListRules(context.Background())
		return len(rules) == 1
	})
	second, err := svc.Ingest(context.Background(), Request{
		URL: "https://shop.example/api/orders/99", Method: "GET", Status: 200,
		ContentType: "application/json", Body: body, CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Mode != "parsed" || second.LLM != "skipped" || second.Rows != 2 || second.Table != "shop_orders" {
		t.Fatalf("%+v", second)
	}
	if model.Calls() != 1 {
		t.Fatalf("llm calls = %d", model.Calls())
	}
	f, err := os.Open(filepath.Join(dir, "tables", "shop_orders.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := records[0]
	joined := map[string]bool{}
	for _, h := range header {
		joined[h] = true
	}
	for _, col := range []string{"order_id", "_captured_at", "_source_url", "_rule_id", "_rule_version"} {
		if !joined[col] {
			t.Fatalf("missing %s in %#v", col, header)
		}
	}
}

func TestRepairKeepsPreviousRuleWhenInvalid(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.UpsertRule(context.Background(), rule.StoredRule{
		Pattern: "https://shop.example/api/orders/*",
		Rule: rule.Rule{
			Pattern: "https://shop.example/api/orders/*", Table: "shop_orders",
			RecordPath: "$.nope[*]",
			Fields:     []rule.Field{{Name: "order_id", Type: "string", From: "$.id", Required: true}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{body: `{"rule":{"pattern":"https://shop.example/api/orders/*","table":"nope","fields":[]}}`}
	svc := New(config.Config{
		Storage: "csv", MaxBodyBytes: 1 << 20, LLMBaseURL: "http://llm.invalid", LLMModel: "mock", LLMTimeout: time.Second, LLMMaxSample: 1000,
	}, store, model, nil)
	res, err := svc.Ingest(context.Background(), Request{
		URL: "https://shop.example/api/orders/1", Method: "GET", Status: 200,
		ContentType: "application/json", Body: `{"data":{"items":[{"id":"a"}]}}`, CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "raw" || res.LLM != "scheduled" {
		t.Fatalf("%+v", res)
	}
	waitFor(t, func() bool {
		buf, err := os.ReadFile(filepath.Join(dir, "rule_jobs.jsonl"))
		return err == nil && bytes.Contains(buf, []byte(`"failed"`))
	})
	rules, err := store.ListRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Version != saved.Version || rules[0].Rule.Table != "shop_orders" {
		buf, _ := json.Marshal(rules)
		t.Fatalf("rule changed: %s", buf)
	}
}

func TestLLMTimeoutRetriesOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{
		failOnce: context.DeadlineExceeded,
		body: `{
		  "rule": {
		    "pattern": "https://shop.example/api/orders/*",
		    "record_path": "$.data.items[*]",
		    "table": "shop_orders",
		    "fields": [{"name":"order_id","type":"string","from":"$.id","required":true}]
		  }
		}`,
	}
	svc := New(config.Config{
		Storage: "csv", MaxBodyBytes: 1 << 20, LLMBaseURL: "http://llm.invalid", LLMModel: "mock", LLMTimeout: time.Second, LLMMaxSample: 1000,
	}, store, model, nil)
	res, err := svc.Ingest(context.Background(), Request{
		URL: "https://shop.example/api/orders/9", Method: "GET", Status: 200,
		ContentType: "application/json", Body: `{"data":{"items":[{"id":"a"}]}}`, CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LLM != "scheduled" {
		t.Fatalf("%+v", res)
	}
	waitFor(t, func() bool {
		rules, _ := store.ListRules(context.Background())
		return len(rules) == 1
	})
	if model.Calls() != 2 {
		t.Fatalf("calls = %d", model.Calls())
	}
}

func TestEmptyBodySkipsLLM(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{body: `{"rule":{"pattern":"https://www.tiktok.com/api/related/item_list/*","table":"tiktok_related_items","fields":[{"name":"id","type":"string","from":"$.id"}]}}`}
	svc := New(config.Config{
		Storage: "csv", MaxBodyBytes: 1 << 20, LLMBaseURL: "http://llm.invalid", LLMModel: "mock", LLMTimeout: time.Second, LLMMaxSample: 1000,
	}, store, model, nil)
	res, err := svc.Ingest(context.Background(), Request{
		URL: "https://www.tiktok.com/api/related/item_list/?id=1", Method: "GET", Status: 200,
		ContentType: "application/json", Body: "   ", CapturedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "raw" || res.LLM != "skipped" {
		t.Fatalf("%+v", res)
	}
	time.Sleep(40 * time.Millisecond)
	if model.Calls() != 0 {
		t.Fatalf("calls = %d", model.Calls())
	}
}

func TestInflightDedup(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	model := &scriptedLLM{
		block: release,
		body: `{
		  "rule": {
		    "pattern": "https://shop.example/api/orders/*",
		    "record_path": "$.data.items[*]",
		    "table": "shop_orders",
		    "fields": [{"name":"order_id","type":"string","from":"$.id","required":true}]
		  }
		}`,
	}
	svc := New(config.Config{
		Storage: "csv", MaxBodyBytes: 1 << 20, LLMBaseURL: "http://llm.invalid", LLMModel: "mock", LLMTimeout: 2 * time.Second, LLMMaxSample: 1000,
	}, store, model, nil)
	req := Request{
		URL: "https://shop.example/api/orders/5", Method: "GET", Status: 200,
		ContentType: "application/json", Body: `{"data":{"items":[{"id":"a"}]}}`, CapturedAt: time.Now(),
	}
	if _, err := svc.Ingest(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return model.Calls() >= 1 })
	time.Sleep(30 * time.Millisecond)
	if model.Calls() != 1 {
		t.Fatalf("calls = %d", model.Calls())
	}
	close(release)
	waitFor(t, func() bool {
		rules, _ := store.ListRules(context.Background())
		return len(rules) == 1
	})
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out")
}
