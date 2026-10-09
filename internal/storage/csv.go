package storage

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"responseforge/internal/rule"
)

type rulesFile struct {
	Rules []rule.StoredRule `json:"rules"`
}

// CSVStore writes rules, raw rows, and parsed tables as files.
type CSVStore struct {
	mu      sync.Mutex
	dir     string
	rules   []rule.StoredRule
	running map[string]jobRun
}

type jobRun struct {
	id        string
	startedAt time.Time
}

func OpenCSV(dir string) (*CSVStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tables"), 0o755); err != nil {
		return nil, err
	}
	s := &CSVStore{dir: dir, running: map[string]jobRun{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *CSVStore) Mode() string { return "csv" }

func (s *CSVStore) load() error {
	b, err := os.ReadFile(filepath.Join(s.dir, "rules.json"))
	if err != nil {
		if os.IsNotExist(err) {
			s.rules = nil
			return nil
		}
		return err
	}
	var file rulesFile
	if len(bytesTrim(b)) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return fmt.Errorf("rules.json: %w", err)
	}
	s.rules = file.Rules
	return nil
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func (s *CSVStore) saveLocked() error {
	buf, err := json.MarshalIndent(rulesFile{Rules: s.rules}, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := filepath.Join(s.dir, "rules.json.tmp")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "rules.json"))
}

func (s *CSVStore) ListRules(ctx context.Context) ([]rule.StoredRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]rule.StoredRule, len(s.rules))
	copy(out, s.rules)
	return out, nil
}

func (s *CSVStore) UpsertRule(ctx context.Context, incoming rule.StoredRule) (rule.StoredRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for i, existing := range s.rules {
		if existing.Pattern == incoming.Pattern {
			existing.Rule = incoming.Rule
			existing.Version++
			existing.UpdatedAt = now
			s.rules[i] = existing
			if err := s.saveLocked(); err != nil {
				return rule.StoredRule{}, err
			}
			return existing, nil
		}
	}
	incoming.ID = newID()
	incoming.Version = 1
	incoming.CreatedAt = now
	incoming.UpdatedAt = now
	incoming.Pattern = incoming.Rule.Pattern
	s.rules = append(s.rules, incoming)
	if err := s.saveLocked(); err != nil {
		return rule.StoredRule{}, err
	}
	return incoming, nil
}

func (s *CSVStore) InsertRaw(ctx context.Context, ev RawEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	header := []string{"captured_at", "url", "method", "status", "content_type", "body", "truncated", "error"}
	row := []string{
		ev.CapturedAt.UTC().Format(time.RFC3339),
		ev.URL,
		ev.Method,
		strconv.Itoa(ev.Status),
		ev.ContentType,
		ev.Body,
		strconv.FormatBool(ev.Truncated),
		ev.Error,
	}
	return appendCSV(filepath.Join(s.dir, "raw.csv"), header, [][]string{row})
}

func (s *CSVStore) InsertRows(ctx context.Context, table string, columns []string, rows []map[string]any) error {
	if err := checkIdents(table, columns); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([][]string, 0, len(rows))
	for _, row := range rows {
		rec := make([]string, len(columns))
		for i, col := range columns {
			rec[i] = formatCell(row[col])
		}
		records = append(records, rec)
	}
	path, err := s.resolveTablePath(table, columns)
	if err != nil {
		return err
	}
	return appendCSV(path, columns, records)
}

func (s *CSVStore) resolveTablePath(table string, header []string) (string, error) {
	dir := filepath.Join(s.dir, "tables")
	for n := 1; n < 100; n++ {
		name := table + ".csv"
		if n > 1 {
			name = fmt.Sprintf("%s.v%d.csv", table, n)
		}
		path := filepath.Join(dir, name)
		existing, err := readHeader(path)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return path, nil
		}
		if headersEqual(existing, header) {
			return path, nil
		}
	}
	return "", fmt.Errorf("too many csv versions for %s", table)
}

func (s *CSVStore) TableExists(ctx context.Context, table string) (bool, error) {
	return false, nil
}

func (s *CSVStore) TableColumns(ctx context.Context, table string) (map[string]bool, error) {
	return nil, nil
}

func (s *CSVStore) ExecCreate(ctx context.Context, statement string, r rule.Rule) error {
	return nil
}

func (s *CSVStore) TryStartJob(ctx context.Context, pattern string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run, ok := s.running[pattern]; ok {
		if time.Since(run.startedAt) < 2*time.Minute {
			return "", false, nil
		}
		_ = s.appendJob(run.id, pattern, "failed", "stale")
		delete(s.running, pattern)
	}
	id := newID()
	s.running[pattern] = jobRun{id: id, startedAt: time.Now()}
	if err := s.appendJob(id, pattern, "running", ""); err != nil {
		delete(s.running, pattern)
		return "", false, err
	}
	return id, true, nil
}

func (s *CSVStore) FinishJob(ctx context.Context, id, status, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pattern, run := range s.running {
		if run.id == id {
			delete(s.running, pattern)
			break
		}
	}
	return s.appendJob(id, "", status, truncateErr(errMsg))
}

func (s *CSVStore) appendJob(id, pattern, status, errMsg string) error {
	f, err := os.OpenFile(filepath.Join(s.dir, "rule_jobs.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	rec := map[string]any{
		"id": id, "pattern": pattern, "status": status, "error": errMsg,
		"at": time.Now().UTC().Format(time.RFC3339),
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	_, err = f.Write(buf)
	return err
}

func appendCSV(path string, header []string, rows [][]string) error {
	newFile := true
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		newFile = false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if newFile {
		if err := w.Write(header); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func readHeader(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rec, err := r.Read()
	if err != nil {
		return nil, nil
	}
	return rec, nil
}

func headersEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// flattenNewlines keeps one CSV record on one physical line. Line breaks inside a
// field, such as a profile signature, become a single space.
func flattenNewlines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " ")
}

func formatCell(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return flattenNewlines(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func checkIdents(table string, columns []string) error {
	if !ruleNameOK(table) {
		return fmt.Errorf("invalid table name")
	}
	for _, c := range columns {
		if !ruleNameOK(c) {
			return fmt.Errorf("invalid column name")
		}
	}
	return nil
}

func ruleNameOK(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
