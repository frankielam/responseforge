package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"responseforge/internal/config"
	"responseforge/internal/ingest"
	"responseforge/internal/rule"
)

// API is the HTTP surface shared by the Go server.
type API struct {
	cfg config.Config
	svc *ingest.Service
}

func New(cfg config.Config, svc *ingest.Service) *API {
	return &API{cfg: cfg, svc: svc}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/ingest", a.ingest)
	mux.HandleFunc("GET /api/v1/rules", a.listRules)
	mux.HandleFunc("PUT /api/v1/rules", a.putRule)
	mux.HandleFunc("POST /api/v1/rules/regenerate", a.regenerate)
	return mux
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"storage": a.svc.StorageMode(),
		"llm":     a.svc.LLMEnabled(),
	})
}

func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	var payload struct {
		Protocol    *int   `json:"protocol"`
		URL         string `json:"url"`
		Method      string `json:"method"`
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
		CapturedAt  string `json:"captured_at"`
		Truncated   bool   `json:"truncated"`
	}
	if err := a.readJSON(w, r, &payload); err != nil {
		return
	}
	if !protocolOK(payload.Protocol, r.Header.Get("Accept")) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "unsupported protocol version", "protocol": config.ProtocolVersion,
		})
		return
	}
	captured := time.Now().UTC()
	if strings.TrimSpace(payload.CapturedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, payload.CapturedAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captured_at must be RFC3339"})
			return
		}
		captured = parsed
	}
	result, err := a.svc.Ingest(r.Context(), ingest.Request{
		URL:         payload.URL,
		Method:      payload.Method,
		Status:      payload.Status,
		ContentType: payload.ContentType,
		Body:        payload.Body,
		CapturedAt:  captured,
		Truncated:   payload.Truncated,
	})
	if err != nil {
		a.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (a *API) listRules(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	rules, err := a.svc.ListRules(r.Context())
	if err != nil {
		a.writeErr(w, err)
		return
	}
	if rules == nil {
		rules = []rule.StoredRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (a *API) putRule(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	body, err := a.readBody(w, r)
	if err != nil {
		return
	}
	var envelope struct {
		Protocol    *int            `json:"protocol"`
		Rule        json.RawMessage `json:"rule"`
		SampleBody  string          `json:"sample_body"`
		ContentType string          `json:"content_type"`
		Pattern     string          `json:"pattern"`
		Fields      json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if !protocolOK(envelope.Protocol, r.Header.Get("Accept")) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "unsupported protocol version", "protocol": config.ProtocolVersion,
		})
		return
	}
	var parsed rule.Rule
	if len(envelope.Rule) > 0 && string(envelope.Rule) != "null" {
		if err := json.Unmarshal(envelope.Rule, &parsed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid rule"})
			return
		}
	} else {
		if err := json.Unmarshal(body, &parsed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid rule"})
			return
		}
	}
	saved, err := a.svc.PutRule(r.Context(), parsed, envelope.SampleBody, envelope.ContentType)
	if err != nil {
		a.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": saved})
}

func (a *API) regenerate(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	var payload struct {
		Protocol    *int   `json:"protocol"`
		Pattern     string `json:"pattern"`
		URL         string `json:"url"`
		Body        string `json:"body"`
		ContentType string `json:"content_type"`
	}
	if err := a.readJSON(w, r, &payload); err != nil {
		return
	}
	if !protocolOK(payload.Protocol, r.Header.Get("Accept")) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "unsupported protocol version", "protocol": config.ProtocolVersion,
		})
		return
	}
	pat, err := a.svc.Regenerate(r.Context(), payload.Pattern, payload.URL, payload.Body, payload.ContentType)
	if err != nil {
		a.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"llm": "scheduled", "pattern": pat})
}

func (a *API) authorized(r *http.Request) bool {
	if a.cfg.IngestToken == "" {
		if !a.svc.LLMEnabled() {
			return true
		}
		return loopbackRemote(r.RemoteAddr)
	}
	return r.Header.Get("Authorization") == "Bearer "+a.cfg.IngestToken
}

func loopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *API) readJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	body, err := a.readBody(w, r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return err
	}
	return nil
}

func (a *API) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limit := a.cfg.MaxBodyBytes*4 + 65536
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error": "body too large", "max_body_bytes": a.cfg.MaxBodyBytes,
			})
			return nil, err
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return nil, err
	}
	return buf, nil
}

func (a *API) writeErr(w http.ResponseWriter, err error) {
	var httpErr *ingest.Error
	if errors.As(err, &httpErr) {
		payload := map[string]any{"error": httpErr.Message}
		if httpErr.Status == http.StatusRequestEntityTooLarge {
			payload["max_body_bytes"] = httpErr.MaxBody
		}
		writeJSON(w, httpErr.Status, payload)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "store failed"})
}

func protocolOK(value *int, accept string) bool {
	if value != nil && *value != config.ProtocolVersion {
		return false
	}
	lower := strings.ToLower(accept)
	idx := strings.Index(lower, "protocol=")
	if idx < 0 {
		return true
	}
	rest := lower[idx+len("protocol="):]
	if !strings.HasPrefix(rest, "1") {
		return false
	}
	if len(rest) == 1 {
		return true
	}
	switch rest[1] {
	case ' ', ';', ',', '\t':
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
