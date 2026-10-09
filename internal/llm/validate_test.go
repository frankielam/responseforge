package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"responseforge/internal/rule"
)

func sampleRuleJSON() string {
	return `{
	  "rule": {
	    "pattern": "https://shop.example/api/orders/*",
	    "content_types": ["application/json"],
	    "record_path": "$.data.items[*]",
	    "table": "shop_orders",
	    "fields": [
	      {"name": "order_id", "type": "string", "from": "$.id", "required": true},
	      {"name": "amount", "type": "number", "from": "$.total"}
	    ]
	  },
	  "create_table": "CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT, amount DOUBLE PRECISION, _captured_at TEXT, _source_url TEXT, _rule_id TEXT, _rule_version INTEGER)"
	}`
}

func TestValidateAcceptsReplay(t *testing.T) {
	out, err := Validate(sampleRuleJSON(), Options{
		SampleBody:   `{"data":{"items":[{"id":"a","total":3}]}}`,
		ContentType:  "application/json",
		Dialect:      rule.DialectPostgres,
		RequireTable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Rule.Table != "shop_orders" {
		t.Fatal(out.Rule.Table)
	}
}

func TestValidateCoercesModelRule(t *testing.T) {
	raw := `{
	  "rule": {
	    "pattern": "https://www.tiktok.com/api/recommend/item_list/?msToken=old",
	    "content_types": ["application/json"],
	    "table": "tiktok_items",
	    "fields": [
	      {"name": "channelId", "type": "string", "from": "$.channelId", "required": true},
	      {"name": "statusCode", "type": "number", "from": "$.statusCode"},
	      {"name": "status_msg", "type": "string", "from": "$.statusMsg", "required": true}
	    ]
	  }
	}`
	out, err := Validate(raw, Options{
		SampleBody:  `{"channelId":"c1","statusCode":0}`,
		ContentType: "application/json",
		Pattern:     "https://www.tiktok.com/api/recommend/item_list/*",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Rule.Pattern != "https://www.tiktok.com/api/recommend/item_list/*" {
		t.Fatal(out.Rule.Pattern)
	}
	if out.Rule.Fields[0].Name != "channel_id" || out.Rule.Fields[1].Name != "status_code" {
		t.Fatalf("%+v", out.Rule.Fields)
	}
	if out.Rule.Fields[2].Required {
		t.Fatal("empty required field should be optional")
	}
}

func TestValidateRelaxesRequiredWhenSampleHasPartialRows(t *testing.T) {
	raw := `{
  "rule": {
    "pattern": "https://shop.example/api/orders/*",
    "record_path": "$.items[*]",
    "table": "shop_orders",
    "fields": [{"name":"order_id","type":"string","from":"$.id","required":true}]
  }
}`
	out, err := Validate(raw, Options{
		SampleBody:  `{"items":[{"id":"a"},{}]}`,
		ContentType: "application/json",
		Pattern:     "https://shop.example/api/orders/*",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Rule.Fields[0].Required {
		t.Fatal("required field must be relaxed when only some sample rows contain it")
	}
}

func TestValidateRejectsBadOutput(t *testing.T) {
	sample := `{"data":{"items":[{"id":"a","total":3}]}}`
	opts := Options{SampleBody: sample, ContentType: "application/json", Dialect: rule.DialectPostgres, RequireTable: true}
	cases := []string{
		`{"rule":{"pattern":"https://shop.example/api/orders/*","table":"Shop","fields":[{"name":"order_id","type":"string","from":"$.id","required":true}]}}`,
		`{"rule":{"pattern":"https://shop.example/api/orders/*","table":"shop_orders","fields":[{"name":"order_id","type":"string","from":"function(){}","required":true}]}}`,
		`{"rule":{"pattern":"https://shop.example/api/orders/*","table":"shop_orders","fields":[{"name":"order_id","type":"string","from":"$.missing","required":true}]},"create_table":"CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT, _captured_at TEXT, _source_url TEXT, _rule_id TEXT, _rule_version INTEGER)"}`,
		"```json\n" + strings.Replace(sampleRuleJSON(), "CREATE TABLE IF NOT EXISTS shop_orders", "DROP TABLE x; CREATE TABLE IF NOT EXISTS shop_orders", 1) + "\n```",
		`{"rule":` + sampleRuleJSON() + `,"note":"no"}`,
	}
	for i, raw := range cases {
		if _, err := Validate(raw, opts); err == nil {
			t.Fatalf("case %d was accepted", i)
		}
	}
}

func TestHTTPClientUsesMockServer(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"rule\":{}}"}}]}`)
	}))
	defer srv.Close()
	client := NewHTTP(srv.URL, "test-key", "mock-model", 2*time.Second)
	got, err := client.Complete(context.Background(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "rule") {
		t.Fatal(got)
	}
	if sawAuth != "Bearer test-key" {
		t.Fatalf("auth header = %q", sawAuth)
	}
}
