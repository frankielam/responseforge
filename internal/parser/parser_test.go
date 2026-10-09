package parser

import (
	"testing"

	"responseforge/internal/rule"
)

func TestJSONRowsAndRequired(t *testing.T) {
	r := rule.Rule{
		Pattern:    "https://shop.example/api/orders/*",
		RecordPath: "$.data.items[*]",
		Table:      "shop_orders",
		Fields: []rule.Field{
			{Name: "order_id", Type: "string", From: "$.id", Required: true},
			{Name: "amount", Type: "number", From: "$.total"},
			{Name: "note", Type: "string", From: "$.note"},
		},
	}
	body := `{"data":{"items":[{"id":"a","total":"12.5"},{"total":1},{"id":"b","total":"nope","note":"x"}]}}`
	rows, err := Parse(r, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: %#v", rows)
	}
	if rows[0]["order_id"] != "a" || rows[0]["amount"] != 12.5 {
		t.Fatalf("first row %#v", rows[0])
	}
	if rows[1]["order_id"] != "b" || rows[1]["amount"] != nil || rows[1]["note"] != "x" {
		t.Fatalf("coercion row %#v", rows[1])
	}

	empty, err := Parse(r, "application/json", `{"data":{"items":[]}}`)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty array should be zero rows, err=%v rows=%v", err, empty)
	}
	if _, err := Parse(r, "application/json", `{"data":{}}`); err == nil {
		t.Fatal("missing record path must fail")
	}
}

func TestJSONAbsolutePathsAndUnixTime(t *testing.T) {
	r := rule.Rule{
		Pattern:    "https://www.tiktok.com/api/comment/list/*",
		RecordPath: "$.comments[*]",
		Table:      "tiktok_comments",
		Fields: []rule.Field{
			{Name: "cid", Type: "string", From: "$.comments[*].cid"},
			{Name: "text", Type: "string", From: "$.comments.text"},
			{Name: "created", Type: "timestamp", From: "$.create_time"},
		},
	}
	body := `{"comments":[{"cid":"1","text":"hello","create_time":1791078713},{"cid":"2","text":"again","create_time":1791078714}]}`
	rows, err := Parse(r, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["text"] != "hello" || rows[1]["cid"] != "2" || rows[0]["created"] != "1791078713" {
		t.Fatalf("%#v", rows)
	}
}

func TestJSONRecordPathWithoutWildcard(t *testing.T) {
	r := rule.Rule{
		Pattern:    "https://www.tiktok.com/api/comment/list/*",
		RecordPath: "$.comments",
		Table:      "tiktok_comments",
		Fields: []rule.Field{
			{Name: "text", Type: "string", From: "$.text", Required: true},
			{Name: "create_time", Type: "timestamp", From: "$.create_time", Required: true},
			{Name: "user_unique_id", Type: "string", From: "$.user.unique_id", Required: true},
		},
	}
	body := `{"comments":[{"text":"4th","create_time":1786195688,"user":{"unique_id":"blurrr.jj"}},{"text":"again","create_time":1786194897,"user":{"unique_id":"froni.k"}}]}`
	rows, err := Parse(r, "application/json; charset=utf-8", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["text"] != "4th" || rows[0]["user_unique_id"] != "blurrr.jj" || rows[1]["create_time"] != "1786194897" {
		t.Fatalf("%#v", rows)
	}
}

func TestHTMLAndRegex(t *testing.T) {
	htmlRule := rule.Rule{
		Pattern:    "https://example.com/page*",
		RecordPath: "div.item",
		Table:      "items",
		Fields: []rule.Field{
			{Name: "name", Type: "string", From: "span.name", Required: true},
			{Name: "item_id", Type: "string", From: "a.id", Attr: "data-id"},
		},
	}
	body := `<div class="item"><span class="name">Ada</span><a class="id" data-id="9">x</a></div>`
	rows, err := Parse(htmlRule, "text/html", body)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["name"] != "Ada" || rows[0]["item_id"] != "9" {
		t.Fatalf("%#v", rows)
	}

	textRule := rule.Rule{
		Pattern: "https://example.com/text*",
		Table:   "lines",
		Fields: []rule.Field{
			{Name: "item_id", Type: "string", From: `id=(?P<item_id>\d+)`, Required: true},
			{Name: "name", Type: "string", From: `name=(?<name>[A-Za-z]+)`},
		},
	}
	rows, err = Parse(textRule, "text/plain", "id=42;name=Ada")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["item_id"] != "42" || rows[0]["name"] != "Ada" {
		t.Fatalf("%#v", rows)
	}
}

func TestTikTokExampleShape(t *testing.T) {
	creators := rule.Rule{
		Pattern:    "https://partner.example/affiliate/creators*",
		RecordPath: "$.data.creators[*]",
		Table:      "tiktok_creators",
		Fields: []rule.Field{
			{Name: "creator_id", Type: "string", From: "$.creator_id", Required: true},
			{Name: "nickname", Type: "string", From: "$.nickname"},
			{Name: "region", Type: "string", From: "$.region"},
			{Name: "follower_count", Type: "number", From: "$.follower_count"},
			{Name: "categories", Type: "string", From: "$.categories"},
		},
	}
	body := `{"data":{"creators":[{"creator_id":"c1","nickname":"Ann","region":"US","follower_count":10,"categories":["beauty","home"]}]}}`
	rows, err := Parse(creators, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["creator_id"] != "c1" || rows[0]["follower_count"] != float64(10) || rows[0]["categories"] != `["beauty","home"]` {
		t.Fatalf("%#v", rows[0])
	}
}
