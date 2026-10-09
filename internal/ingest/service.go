package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"responseforge/internal/config"
	"responseforge/internal/llm"
	"responseforge/internal/parser"
	"responseforge/internal/pattern"
	"responseforge/internal/rule"
	"responseforge/internal/storage"
)

// Request is one extension upload.
type Request struct {
	URL         string
	Method      string
	Status      int
	ContentType string
	Body        string
	CapturedAt  time.Time
	Truncated   bool
}

// Result is the ingest response body.
type Result struct {
	Stored  bool   `json:"stored"`
	Mode    string `json:"mode"`
	Pattern string `json:"pattern,omitempty"`
	Table   string `json:"table,omitempty"`
	Rows    int    `json:"rows"`
	LLM     string `json:"llm"`
}

// Service matches rules, stores rows, and schedules LLM work.
type Service struct {
	cfg   config.Config
	store storage.Store
	model llm.Client
	log   *slog.Logger
}

func New(cfg config.Config, store storage.Store, model llm.Client, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: cfg, store: store, model: model, log: log}
}

func (s *Service) LLMEnabled() bool {
	return s.cfg.LLMEnabled() && s.model != nil
}

func (s *Service) StorageMode() string {
	if s.store.Mode() == "postgres" {
		return "postgres"
	}
	return "csv"
}

func (s *Service) Ingest(ctx context.Context, req Request) (Result, error) {
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if err := validateRequest(req, s.cfg.MaxBodyBytes); err != nil {
		return Result{}, err
	}
	rules, err := s.store.ListRules(ctx)
	if err != nil {
		return Result{}, err
	}
	candidates := make([]pattern.Candidate, len(rules))
	for i, r := range rules {
		candidates[i] = pattern.Candidate{
			Pattern: r.Pattern, ContentTypes: r.Rule.ContentTypes, UpdatedAt: r.UpdatedAt,
		}
	}
	if idx, ok := pattern.Select(candidates, req.URL, req.ContentType); ok {
		matched := rules[idx]
		rows, perr := parser.Parse(matched.Rule, req.ContentType, req.Body)
		if perr != nil {
			if err := s.writeRaw(ctx, req, perr.Error()); err != nil {
				return Result{}, err
			}
			llmState := "disabled"
			if s.LLMEnabled() && sampleUsable(req.Body) {
				llmState = "scheduled"
				sample := req
				saved := matched
				reason := perr.Error()
				go s.generate(saved.Pattern, sample, &saved, reason)
			} else if s.LLMEnabled() {
				llmState = "skipped"
			}
			return Result{
				Stored: true, Mode: "raw", Pattern: matched.Pattern, Table: "raw_events",
				Rows: 1, LLM: llmState,
			}, nil
		}
		if err := s.writeParsed(ctx, matched, req, rows); err != nil {
			if rawErr := s.writeRaw(ctx, req, err.Error()); rawErr != nil {
				return Result{}, rawErr
			}
			llmState := "disabled"
			if s.LLMEnabled() {
				llmState = "skipped"
			}
			return Result{
				Stored: true, Mode: "raw", Pattern: matched.Pattern, Table: "raw_events",
				Rows: 1, LLM: llmState,
			}, nil
		}
		return Result{
			Stored: true, Mode: "parsed", Pattern: matched.Pattern, Table: matched.Rule.Table,
			Rows: len(rows), LLM: "skipped",
		}, nil
	}

	if err := s.writeRaw(ctx, req, ""); err != nil {
		return Result{}, err
	}
	llmState := "disabled"
	if s.LLMEnabled() && sampleUsable(req.Body) {
		llmState = "scheduled"
		normalized, nerr := pattern.Normalize(req.URL)
		if nerr != nil {
			llmState = "disabled"
		} else {
			sample := req
			go s.generate(normalized, sample, nil, "")
		}
	} else if s.LLMEnabled() {
		llmState = "skipped"
	}
	return Result{Stored: true, Mode: "raw", Table: "raw_events", Rows: 1, LLM: llmState}, nil
}

func (s *Service) ListRules(ctx context.Context) ([]rule.StoredRule, error) {
	return s.store.ListRules(ctx)
}

func (s *Service) PutRule(ctx context.Context, r rule.Rule, sampleBody, contentType string) (rule.StoredRule, error) {
	if err := r.Validate(); err != nil {
		return rule.StoredRule{}, &Error{Status: 400, Message: err.Error()}
	}
	if strings.TrimSpace(sampleBody) != "" {
		if contentType == "" {
			contentType = "application/json"
		}
		if _, err := parser.Parse(r, contentType, sampleBody); err != nil {
			return rule.StoredRule{}, &Error{Status: 400, Message: "replay failed: " + err.Error()}
		}
	}
	if s.store.Mode() == "postgres" {
		if err := s.ensureTable(ctx, r); err != nil {
			if strings.Contains(err.Error(), "missing column") {
				return rule.StoredRule{}, &Error{Status: 400, Message: err.Error()}
			}
			return rule.StoredRule{}, err
		}
	}
	saved, err := s.store.UpsertRule(ctx, rule.StoredRule{Pattern: r.Pattern, Rule: r})
	if err != nil {
		return rule.StoredRule{}, err
	}
	return saved, nil
}

// Regenerate schedules a forced LLM run for a pattern. It does not wait.
func (s *Service) Regenerate(ctx context.Context, rawPattern, rawURL, body, contentType string) (string, error) {
	if !s.LLMEnabled() {
		return "", &Error{Status: 400, Message: "llm disabled"}
	}
	pat := strings.TrimSpace(rawPattern)
	if pat == "" {
		var err error
		pat, err = pattern.Normalize(rawURL)
		if err != nil {
			return "", &Error{Status: 400, Message: "pattern or url is required"}
		}
	}
	if strings.TrimSpace(body) == "" {
		return "", &Error{Status: 400, Message: "sample body is required"}
	}
	if contentType == "" {
		contentType = "application/json"
	}
	if !pattern.Textual(contentType) {
		return "", &Error{Status: 415, Message: "unsupported content type"}
	}
	rules, err := s.store.ListRules(ctx)
	if err != nil {
		return "", err
	}
	var old *rule.StoredRule
	for i := range rules {
		if rules[i].Pattern == pat {
			old = &rules[i]
			break
		}
	}
	req := Request{URL: rawURL, Method: "GET", Status: 200, ContentType: contentType, Body: body, CapturedAt: time.Now().UTC()}
	if req.URL == "" {
		req.URL = pat
	}
	go s.generate(pat, req, old, "manual regenerate")
	return pat, nil
}

func sampleUsable(body string) bool {
	return strings.TrimSpace(body) != ""
}

func (s *Service) writeRaw(ctx context.Context, req Request, reason string) error {
	return s.store.InsertRaw(ctx, storage.RawEvent{
		CapturedAt:  req.CapturedAt,
		URL:         req.URL,
		Method:      req.Method,
		Status:      req.Status,
		ContentType: req.ContentType,
		Body:        req.Body,
		Truncated:   req.Truncated,
		Error:       reason,
	})
}

func (s *Service) writeParsed(ctx context.Context, saved rule.StoredRule, req Request, rows []map[string]any) error {
	if s.store.Mode() == "postgres" {
		if err := s.ensureTable(ctx, saved.Rule); err != nil {
			return err
		}
	}
	captured := req.CapturedAt.UTC().Format(time.RFC3339)
	for i := range rows {
		rows[i]["_captured_at"] = captured
		rows[i]["_source_url"] = req.URL
		rows[i]["_rule_id"] = saved.ID
		rows[i]["_rule_version"] = saved.Version
	}
	return s.store.InsertRows(ctx, saved.Rule.Table, saved.Rule.ColumnNames(), rows)
}

func (s *Service) ensureTable(ctx context.Context, r rule.Rule) error {
	exists, err := s.store.TableExists(ctx, r.Table)
	if err != nil {
		return err
	}
	if exists {
		columns, err := s.store.TableColumns(ctx, r.Table)
		if err != nil {
			return err
		}
		for _, column := range r.ColumnNames() {
			if !columns[column] {
				return fmt.Errorf("table %s is missing column %s", r.Table, column)
			}
		}
		return nil
	}
	statement, err := rule.CreateTableSQL(r, rule.DialectPostgres)
	if err != nil {
		return err
	}
	return s.store.ExecCreate(ctx, statement, r)
}

func (s *Service) generate(pat string, sample Request, old *rule.StoredRule, failure string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*s.cfg.LLMTimeout+5*time.Second)
	defer cancel()
	id, started, err := s.store.TryStartJob(ctx, pat)
	if err != nil || !started {
		return
	}
	status := "succeeded"
	errMsg := ""
	defer func() {
		if rec := recover(); rec != nil {
			status = "failed"
			errMsg = fmt.Sprintf("panic: %v", rec)
			s.log.Error("llm job panic", "pattern", pat)
		}
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer finishCancel()
		if ferr := s.store.FinishJob(finishCtx, id, status, errMsg); ferr != nil {
			s.log.Error("finish job", "error", ferr.Error())
		}
	}()
	if err := s.runModel(ctx, pat, sample, old, failure); err != nil {
		status = "failed"
		errMsg = err.Error()
		s.log.Error("llm job failed", "pattern", pat, "error", errMsg)
	}
}

func (s *Service) runModel(ctx context.Context, pat string, sample Request, old *rule.StoredRule, failure string) error {
	dialect := rule.DialectSQLite
	database := s.store.Mode() == "postgres"
	if database {
		dialect = rule.DialectPostgres
	}
	var oldRule *rule.Rule
	if old != nil {
		copied := old.Rule
		oldRule = &copied
	}
	prompt := llm.BuildPrompt(llm.PromptInput{
		URL:            sample.URL,
		Pattern:        pat,
		ContentType:    sample.ContentType,
		Body:           sample.Body,
		Dialect:        dialect,
		Database:       database,
		NeedCreate:     database,
		OldRule:        oldRule,
		Failure:        failure,
		MaxSampleChars: s.cfg.LLMMaxSample,
	})
	text, err := s.complete(ctx, prompt)
	if err != nil {
		return err
	}
	out, err := llm.Validate(text, llm.Options{
		SampleBody:  sample.Body,
		ContentType: sample.ContentType,
		Dialect:     dialect,
		Pattern:     pat,
	})
	if err != nil {
		s.log.Warn("llm rule rejected", "pattern", pat, "error", err.Error())
		retryOld := oldRule
		if retryOld == nil {
			retryOld = rejectedRule(text)
		}
		retry := llm.BuildPrompt(llm.PromptInput{
			URL:            sample.URL,
			Pattern:        pat,
			ContentType:    sample.ContentType,
			Body:           sample.Body,
			Dialect:        dialect,
			Database:       database,
			NeedCreate:     database,
			OldRule:        retryOld,
			Failure:        err.Error() + ". Field paths must match the sample. For an array, record_path is the array and from is relative to one element.",
			MaxSampleChars: s.cfg.LLMMaxSample,
		})
		text, err = s.complete(ctx, retry)
		if err != nil {
			return err
		}
		out, err = llm.Validate(text, llm.Options{
			SampleBody:  sample.Body,
			ContentType: sample.ContentType,
			Dialect:     dialect,
			Pattern:     pat,
		})
		if err != nil {
			return err
		}
	}
	if oldRule != nil && out.Rule.Table != oldRule.Table {
		return fmt.Errorf("repair must keep table %s", oldRule.Table)
	}
	if database {
		exists, err := s.store.TableExists(ctx, out.Rule.Table)
		if err != nil {
			return err
		}
		if !exists {
			statement, err := rule.CreateTableSQL(out.Rule, dialect)
			if err != nil {
				return err
			}
			if err := s.store.ExecCreate(ctx, statement, out.Rule); err != nil {
				return err
			}
			if err := s.ensureTable(ctx, out.Rule); err != nil {
				return err
			}
		} else if err := s.ensureTable(ctx, out.Rule); err != nil {
			return err
		}
	}
	_, err = s.store.UpsertRule(ctx, rule.StoredRule{Pattern: pat, Rule: out.Rule})
	return err
}

func (s *Service) complete(ctx context.Context, prompt string) (string, error) {
	text, err := s.completeOnce(ctx, prompt)
	if err == nil || !retryableLLM(err) {
		return text, err
	}
	s.log.Warn("llm request retry", "error", err.Error())
	return s.completeOnce(ctx, prompt)
}

func (s *Service) completeOnce(ctx context.Context, prompt string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.LLMTimeout)
	defer cancel()
	return s.model.Complete(callCtx, prompt)
}

func retryableLLM(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "Client.Timeout")
}

func rejectedRule(raw string) *rule.Rule {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}
	var out llm.Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out.Rule.Fields) == 0 {
		return nil
	}
	return &out.Rule
}

func validateRequest(req Request, maxBody int64) error {
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &Error{Status: 400, Message: "url must be an absolute http(s) url"}
	}
	if len(req.URL) > 4096 {
		return &Error{Status: 400, Message: "url is too long"}
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" || len(method) > 16 {
		return &Error{Status: 400, Message: "method is required"}
	}
	for _, r := range method {
		if r < 'A' || r > 'Z' {
			return &Error{Status: 400, Message: "method is invalid"}
		}
	}
	if req.Status < 100 || req.Status > 599 {
		return &Error{Status: 400, Message: "status is invalid"}
	}
	if !pattern.Textual(req.ContentType) {
		return &Error{Status: 415, Message: "unsupported content type"}
	}
	if int64(len(req.Body)) > maxBody {
		return &Error{Status: 413, Message: "body too large", MaxBody: maxBody}
	}
	return nil
}

// Error is an HTTP-mapped failure.
type Error struct {
	Status  int
	Message string
	MaxBody int64
}

func (e *Error) Error() string { return e.Message }
