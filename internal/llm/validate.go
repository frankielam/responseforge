package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"responseforge/internal/parser"
	"responseforge/internal/rule"
)

// Output is the only object a model is allowed to return.
type Output struct {
	Rule        rule.Rule `json:"rule"`
	CreateTable string    `json:"create_table,omitempty"`
}

// Options controls replay and SQL checks.
type Options struct {
	SampleBody   string
	ContentType  string
	Dialect      string
	RequireTable bool
	// Pattern replaces the model pattern. The server pattern is the one that is saved.
	Pattern string
}

// Validate checks schema, replays the rule on the sample, and checks DDL.
func Validate(raw string, opts Options) (Output, error) {
	raw = stripFence(raw)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return Output{}, fmt.Errorf("llm output is not json")
	}
	for key := range probe {
		if key != "rule" && key != "create_table" {
			return Output{}, fmt.Errorf("llm output has unexpected field %s", key)
		}
	}
	var out Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Output{}, fmt.Errorf("llm output is not a rule")
	}
	if opts.Pattern != "" {
		out.Rule.Pattern = opts.Pattern
	}
	normalizeFields(&out.Rule)
	if err := out.Rule.Validate(); err != nil {
		return Output{}, err
	}
	rows, err := parser.Parse(out.Rule, opts.ContentType, opts.SampleBody)
	if err != nil && strings.Contains(err.Error(), "required field") {
		clearRequired(&out.Rule)
		rows, err = parser.Parse(out.Rule, opts.ContentType, opts.SampleBody)
	}
	if err != nil {
		return Output{}, fmt.Errorf("replay failed: %w", err)
	}
	if len(rows) > 0 && hasRequired(out.Rule) {
		optional := out.Rule
		optional.Fields = append([]rule.Field(nil), out.Rule.Fields...)
		clearRequired(&optional)
		allRows, optionalErr := parser.Parse(optional, opts.ContentType, opts.SampleBody)
		if optionalErr == nil && len(allRows) > len(rows) {
			clearRequired(&out.Rule)
			rows = allRows
		}
	}
	if len(rows) == 0 && strings.TrimSpace(opts.SampleBody) != "" {
		return Output{}, fmt.Errorf("replay produced no rows for a non-empty sample")
	}
	relaxRequired(&out.Rule, rows)
	if !anyValue(rows) {
		return Output{}, fmt.Errorf("replay extracted no values")
	}
	if opts.RequireTable {
		if err := rule.ValidateCreateTable(out.CreateTable, out.Rule, opts.Dialect); err != nil {
			return Output{}, err
		}
	}
	return out, nil
}

func normalizeFields(r *rule.Rule) {
	used := map[string]bool{}
	for i := range r.Fields {
		name := snakeField(r.Fields[i].Name)
		for _, reserved := range rule.SystemColumns {
			if name == reserved {
				name = "field_" + strings.TrimPrefix(name, "_")
			}
		}
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s_%d", base, n)
			if len(name) > 63 {
				name = name[:63]
			}
		}
		used[name] = true
		r.Fields[i].Name = name
	}
}

func snakeField(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	prevUnderscore := false
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && !prevUnderscore {
				b.WriteByte('_')
			}
			b.WriteByte(byte(r + ('a' - 'A')))
			prevUnderscore = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if b.Len() > 0 && !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		s = "f_" + s
		s = strings.Trim(s, "_")
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "_")
	}
	return s
}

func clearRequired(r *rule.Rule) {
	for i := range r.Fields {
		r.Fields[i].Required = false
	}
}

func hasRequired(r rule.Rule) bool {
	for _, field := range r.Fields {
		if field.Required {
			return true
		}
	}
	return false
}

func relaxRequired(r *rule.Rule, rows []map[string]any) {
	for i, f := range r.Fields {
		if !f.Required {
			continue
		}
		present := false
		for _, row := range rows {
			if !emptyValue(row[f.Name]) {
				present = true
				break
			}
		}
		if !present {
			r.Fields[i].Required = false
		}
	}
}

func anyValue(rows []map[string]any) bool {
	for _, row := range rows {
		for _, value := range row {
			if !emptyValue(value) {
				return true
			}
		}
	}
	return false
}

func emptyValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "json") {
		s = strings.TrimSpace(s[4:])
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
