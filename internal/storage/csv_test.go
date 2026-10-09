package storage

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInsertRowsFlattensNewlines(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenCSV(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertRows(context.Background(), "items", []string{"id", "signature"}, []map[string]any{
		{"id": "1", "signature": "dm for promo\nAE*24"},
		{"id": "2", "signature": "line one\r\n\r\nline two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "tables", "items.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text, err := os.ReadFile(filepath.Join(dir, "tables", "items.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(text), "\n") != 3 {
		t.Fatalf("expected one line per record, got %q", text)
	}
	rows, err := csv.NewReader(strings.NewReader(string(text))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][1] != "dm for promo AE*24" || rows[2][1] != "line one line two" {
		t.Fatalf("%#v", rows)
	}
}
