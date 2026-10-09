package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"responseforge/internal/config"
	"responseforge/internal/ingest"
	"responseforge/internal/storage"
)

func newAPI(t *testing.T, token string) *API {
	t.Helper()
	store, err := storage.OpenCSV(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := ingest.New(config.Config{Storage: "csv", MaxBodyBytes: 32, IngestToken: token}, store, nil, nil)
	return New(config.Config{IngestToken: token, MaxBodyBytes: 32}, svc)
}

func TestHealthAndAuth(t *testing.T) {
	api := newAPI(t, "secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"storage":"csv"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"llm":false`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	body := bytes.NewBufferString(`{"url":"https://example.com/a","method":"GET","status":200,"content_type":"application/json","body":"{}","captured_at":"2026-10-04T02:16:00+08:00"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ingest", body)
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rec.Code)
	}
	body = bytes.NewBufferString(`{"protocol":1,"url":"https://example.com/a","method":"GET","status":200,"content_type":"application/octet-stream","body":"abc"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ingest", body)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type %d %s", rec.Code, rec.Body.String())
	}
	body = bytes.NewBufferString(`{"protocol":2,"url":"https://example.com/a","method":"GET","status":200,"content_type":"application/json","body":"{}"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ingest", body)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"protocol":1`)) {
		t.Fatalf("protocol %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutAndParsedIngest(t *testing.T) {
	store, err := storage.OpenCSV(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := ingest.New(config.Config{MaxBodyBytes: 1 << 20}, store, nil, nil)
	api := New(config.Config{MaxBodyBytes: 1 << 20}, svc)
	ruleBody := `{
	  "rule": {
	    "pattern": "https://shop.example/api/orders/*",
	    "content_types": ["application/json"],
	    "record_path": "$.data.items[*]",
	    "table": "shop_orders",
	    "fields": [{"name":"order_id","type":"string","from":"$.id","required":true}]
	  },
	  "sample_body": "{\"data\":{\"items\":[{\"id\":\"a\"}]}}",
	  "content_type": "application/json"
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/rules", bytes.NewBufferString(ruleBody))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put %d %s", rec.Code, rec.Body.String())
	}
	payload := map[string]any{
		"protocol": 1, "url": "https://shop.example/api/orders/4", "method": "GET", "status": 200,
		"content_type": "application/json", "body": `{"data":{"items":[{"id":"a"},{"id":"b"}]}}`,
		"captured_at": time.Now().Format(time.RFC3339), "truncated": false,
	}
	buf, _ := json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(buf))
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("ingest %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["mode"] != "parsed" || resp["rows"] != float64(2) {
		t.Fatalf("%s", rec.Body.String())
	}
}
