package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"responseforge/internal/rule"
)

// PostgresStore keeps rules and rows in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, dsn string, timeout time.Duration) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				s := &PostgresStore{pool: pool}
				if migErr := s.migrate(ctx); migErr != nil {
					pool.Close()
					return nil, migErr
				}
				return s, nil
			}
			pool.Close()
		}
		last = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("postgres connection failed: %w", last)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS parse_rules (
			id TEXT PRIMARY KEY,
			pattern TEXT NOT NULL UNIQUE,
			rule JSONB NOT NULL,
			version INTEGER NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS raw_events (
			id BIGSERIAL PRIMARY KEY,
			captured_at TIMESTAMPTZ,
			url TEXT,
			method TEXT,
			status INTEGER,
			content_type TEXT,
			body TEXT,
			truncated BOOLEAN,
			error TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS rule_jobs (
			id TEXT PRIMARY KEY,
			pattern TEXT NOT NULL,
			status TEXT NOT NULL,
			error TEXT,
			created_at TIMESTAMPTZ NOT NULL,
			finished_at TIMESTAMPTZ
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS rule_jobs_one_running ON rule_jobs (pattern) WHERE status = 'running'`,
	}
	for _, stmt := range statements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) Mode() string { return "postgres" }

func (s *PostgresStore) ListRules(ctx context.Context) ([]rule.StoredRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, pattern, rule, version, created_at, updated_at FROM parse_rules ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rule.StoredRule
	for rows.Next() {
		var item rule.StoredRule
		var raw []byte
		if err := rows.Scan(&item.ID, &item.Pattern, &raw, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Rule); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpsertRule(ctx context.Context, incoming rule.StoredRule) (rule.StoredRule, error) {
	now := time.Now().UTC()
	var existingID string
	var version int
	var created time.Time
	err := s.pool.QueryRow(ctx, `SELECT id, version, created_at FROM parse_rules WHERE pattern = $1`, incoming.Pattern).Scan(&existingID, &version, &created)
	buf, mErr := json.Marshal(incoming.Rule)
	if mErr != nil {
		return rule.StoredRule{}, mErr
	}
	if errors.Is(err, pgx.ErrNoRows) {
		incoming.ID = newID()
		incoming.Version = 1
		incoming.CreatedAt = now
		incoming.UpdatedAt = now
		_, err = s.pool.Exec(ctx, `INSERT INTO parse_rules (id, pattern, rule, version, created_at, updated_at) VALUES ($1,$2,$3::jsonb,$4,$5,$6)`,
			incoming.ID, incoming.Pattern, buf, incoming.Version, incoming.CreatedAt, incoming.UpdatedAt)
		return incoming, err
	}
	if err != nil {
		return rule.StoredRule{}, err
	}
	incoming.ID = existingID
	incoming.Version = version + 1
	incoming.CreatedAt = created
	incoming.UpdatedAt = now
	_, err = s.pool.Exec(ctx, `UPDATE parse_rules SET rule = $2::jsonb, version = $3, updated_at = $4 WHERE id = $1`,
		incoming.ID, buf, incoming.Version, incoming.UpdatedAt)
	return incoming, err
}

func (s *PostgresStore) InsertRaw(ctx context.Context, ev RawEvent) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO raw_events (captured_at, url, method, status, content_type, body, truncated, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		ev.CapturedAt.UTC(), ev.URL, ev.Method, ev.Status, ev.ContentType, ev.Body, ev.Truncated, ev.Error)
	return err
}

func (s *PostgresStore) InsertRows(ctx context.Context, table string, columns []string, rows []map[string]any) error {
	if err := checkIdents(table, columns); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	quoted := make([]string, len(columns))
	holders := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = `"` + col + `"`
		holders[i] = fmt.Sprintf("$%d", i+1)
	}
	q := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s)", table, strings.Join(quoted, ","), strings.Join(holders, ","))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, row := range rows {
		vals := make([]any, len(columns))
		for i, col := range columns {
			vals[i] = row[col]
		}
		if _, err := tx.Exec(ctx, q, vals...); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) TableExists(ctx context.Context, table string) (bool, error) {
	if !ruleNameOK(table) {
		return false, fmt.Errorf("invalid table name")
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	)`, table).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) TableColumns(ctx context.Context, table string) (map[string]bool, error) {
	if !ruleNameOK(table) {
		return nil, fmt.Errorf("invalid table name")
	}
	rows, err := s.pool.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *PostgresStore) ExecCreate(ctx context.Context, statement string, r rule.Rule) error {
	if err := rule.ValidateCreateTable(statement, r, rule.DialectPostgres); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, statement)
	return err
}

func (s *PostgresStore) TryStartJob(ctx context.Context, pattern string) (string, bool, error) {
	_, err := s.pool.Exec(ctx, `UPDATE rule_jobs SET status = 'failed', error = 'stale', finished_at = now()
		WHERE pattern = $1 AND status = 'running' AND created_at < now() - interval '2 minutes'`, pattern)
	if err != nil {
		return "", false, err
	}
	id := newID()
	_, err = s.pool.Exec(ctx, `INSERT INTO rule_jobs (id, pattern, status, created_at) VALUES ($1,$2,'running', now())`, id, pattern)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (s *PostgresStore) FinishJob(ctx context.Context, id, status, errMsg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE rule_jobs SET status = $2, error = $3, finished_at = now() WHERE id = $1`, id, status, truncateErr(errMsg))
	return err
}
