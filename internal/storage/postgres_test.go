package storage

import (
	"context"
	"testing"
	"time"
)

func TestPostgresStartupFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := OpenPostgres(ctx, "postgres://edf:edf@127.0.0.1:1/edf?sslmode=disable", 400*time.Millisecond)
	if err == nil {
		t.Fatal("connection failure must not fall back")
	}
}
