package pattern

import (
	"testing"
	"time"
)

func TestMatchAndSpecificity(t *testing.T) {
	url := "https://shop.example/api/orders/1842?page=3"
	if !Match("https://shop.example/api/orders/*", url) {
		t.Fatal("expected wildcard match")
	}
	if Match("https://other.example/*", url) {
		t.Fatal("different host must not match")
	}
	rules := []Candidate{
		{Pattern: "https://shop.example/api/*", UpdatedAt: time.Now()},
		{Pattern: "https://shop.example/api/orders/*", UpdatedAt: time.Now().Add(-time.Hour)},
		{Pattern: "https://shop.example/api/orders/special*", UpdatedAt: time.Now()},
	}
	idx, ok := Select(rules, url, "application/json")
	if !ok || rules[idx].Pattern != "https://shop.example/api/orders/*" {
		t.Fatalf("winner = %v %d", ok, idx)
	}

	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	tied := []Candidate{
		{Pattern: "https://shop.example/api/orders/*", UpdatedAt: older},
		{Pattern: "https://shop.example/api/orders/*", UpdatedAt: newer},
	}
	idx, ok = Select(tied, url, "application/json")
	if !ok || !tied[idx].UpdatedAt.Equal(newer) {
		t.Fatal("tie must prefer the newest rule")
	}
}

func TestContentTypeSkipsRule(t *testing.T) {
	rules := []Candidate{
		{Pattern: "https://shop.example/*", ContentTypes: []string{"text/html"}, UpdatedAt: time.Now()},
		{Pattern: "https://shop.example/api/*", ContentTypes: []string{"application/json"}, UpdatedAt: time.Now().Add(-time.Hour)},
	}
	idx, ok := Select(rules, "https://shop.example/api/orders", "application/json; charset=utf-8")
	if !ok || rules[idx].Pattern != "https://shop.example/api/*" {
		t.Fatalf("content type filter failed: %+v", rules[idx])
	}
}

func TestNormalize(t *testing.T) {
	got, err := Normalize("https://shop.example/api/orders/1842?page=3#frag")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://shop.example/api/orders/*"
	if got != want {
		t.Fatalf("got %s", got)
	}
	got, err = Normalize("https://shop.example/api/users/550e8400-e29b-41d4-a716-446655440000?cursor=abc&region=US")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://shop.example/api/users/*" {
		t.Fatalf("got %s", got)
	}
	got, err = Normalize("https://www.tiktok.com/api/prefetch/explore/item_list/?msToken=abc&device_id=1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://www.tiktok.com/api/prefetch/explore/item_list/*" {
		t.Fatalf("got %s", got)
	}
	if !Match(got, "https://www.tiktok.com/api/prefetch/explore/item_list/?msToken=next&X-Bogus=1") {
		t.Fatal("query variants must match the path pattern")
	}
}

func TestTextual(t *testing.T) {
	if !Textual("application/json") || !Textual("text/html; charset=utf-8") || !Textual("text/csv") || !Textual("application/xml") {
		t.Fatal("expected textual types")
	}
	if Textual("application/octet-stream") || Textual("") {
		t.Fatal("binary and empty types are rejected")
	}
}
