package storage

import (
	"context"
	"time"

	"responseforge/internal/rule"
)

// RawEvent is one unparsed response.
type RawEvent struct {
	CapturedAt  time.Time
	URL         string
	Method      string
	Status      int
	ContentType string
	Body        string
	Truncated   bool
	Error       string
}

// Store persists rules, raw events, parsed rows, and LLM jobs.
type Store interface {
	Mode() string
	ListRules(ctx context.Context) ([]rule.StoredRule, error)
	UpsertRule(ctx context.Context, r rule.StoredRule) (rule.StoredRule, error)
	InsertRaw(ctx context.Context, ev RawEvent) error
	InsertRows(ctx context.Context, table string, columns []string, rows []map[string]any) error
	TableExists(ctx context.Context, table string) (bool, error)
	TableColumns(ctx context.Context, table string) (map[string]bool, error)
	ExecCreate(ctx context.Context, statement string, r rule.Rule) error
	TryStartJob(ctx context.Context, pattern string) (id string, started bool, err error)
	FinishJob(ctx context.Context, id, status, errMsg string) error
}
