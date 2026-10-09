package rule

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

var (
	tableNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	fieldNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
	jsonPathRE  = regexp.MustCompile(`^\$(\.[A-Za-z_][A-Za-z0-9_]*|\[['"][^'"\]]+['"]\]|\[\d+\]|\[\*\])*$`)
	dangerousRE = regexp.MustCompile(`(?i)(<script|javascript\s*:|\beval\s*\(|\bfunction\s*\(|\bimport\b|\brequire\b|=>)`)
)

// SystemColumns are added by the server, never by the model.
var SystemColumns = []string{"_captured_at", "_source_url", "_rule_id", "_rule_version"}

// Field is one declarative extractor.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	From     string `json:"from"`
	Required bool   `json:"required,omitempty"`
	Attr     string `json:"attr,omitempty"`
}

// Rule is a declarative parse rule.
type Rule struct {
	Pattern      string   `json:"pattern"`
	ContentTypes []string `json:"content_types,omitempty"`
	RecordPath   string   `json:"record_path,omitempty"`
	Table        string   `json:"table"`
	Fields       []Field  `json:"fields"`
}

// StoredRule is a persisted pattern-to-rule association.
type StoredRule struct {
	ID        string    `json:"id"`
	Pattern   string    `json:"pattern"`
	Rule      Rule      `json:"rule"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the rule schema. It does not execute the rule.
func (r Rule) Validate() error {
	if strings.TrimSpace(r.Pattern) == "" || len(r.Pattern) > 4096 || strings.ContainsAny(r.Pattern, "\r\n") {
		return fmt.Errorf("pattern is required")
	}
	if strings.Contains(r.Pattern, "**") {
		return fmt.Errorf("pattern must not contain **")
	}
	if !tableNameRE.MatchString(r.Table) {
		return fmt.Errorf("table name must match [a-z][a-z0-9_]{0,62}")
	}
	if len(r.Fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}
	if len(r.Fields) > 256 {
		return fmt.Errorf("too many fields")
	}
	seen := map[string]bool{}
	for _, f := range r.Fields {
		if !fieldNameRE.MatchString(f.Name) {
			return fmt.Errorf("invalid field name %q", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate field %q", f.Name)
		}
		seen[f.Name] = true
		switch f.Type {
		case "string", "number", "boolean", "timestamp":
		default:
			return fmt.Errorf("field %s has unsupported type %q", f.Name, f.Type)
		}
		if err := validateFrom(f.From); err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		if strings.ContainsAny(f.Attr, "\r\n\"'<>") {
			return fmt.Errorf("field %s has an invalid attr", f.Name)
		}
	}
	if r.RecordPath != "" {
		if err := validateRecordPath(r.RecordPath); err != nil {
			return err
		}
	}
	for _, ct := range r.ContentTypes {
		if strings.TrimSpace(ct) == "" || strings.ContainsAny(ct, "\r\n") {
			return fmt.Errorf("invalid content type")
		}
	}
	if len(r.ContentTypes) > 64 {
		return fmt.Errorf("too many content types")
	}
	return nil
}

func validateFrom(from string) error {
	if strings.TrimSpace(from) == "" || len(from) > 500 || strings.ContainsAny(from, "\r\n") {
		return fmt.Errorf("from must be a single line of at most 500 characters")
	}
	if dangerousRE.MatchString(from) {
		return fmt.Errorf("from must be a declarative extractor")
	}
	switch classify(from) {
	case "jsonpath":
		if !jsonPathRE.MatchString(from) {
			return fmt.Errorf("jsonpath is not allowed")
		}
	case "regex":
		if _, err := compileUserRegexp(from); err != nil {
			return err
		}
	default:
		if strings.ContainsAny(from, "{};") {
			return fmt.Errorf("css selector must not contain braces or semicolons")
		}
	}
	return nil
}

func validateRecordPath(path string) error {
	if len(path) > 500 || strings.ContainsAny(path, "\r\n") || dangerousRE.MatchString(path) {
		return fmt.Errorf("invalid record_path")
	}
	if strings.HasPrefix(path, "$") && !jsonPathRE.MatchString(path) {
		return fmt.Errorf("record_path jsonpath is not allowed")
	}
	if classify(path) == "regex" {
		if _, err := compileUserRegexp(path); err != nil {
			return fmt.Errorf("record_path: %w", err)
		}
	}
	return nil
}

func classify(from string) string {
	if strings.HasPrefix(from, "$") {
		return "jsonpath"
	}
	if strings.Contains(from, "(?P<") || strings.Contains(from, "(?<") {
		return "regex"
	}
	return "css"
}

// compileUserRegexp accepts one named group in either Go (?P<name>) or JS (?<name>) syntax.
func compileUserRegexp(expr string) (*regexp.Regexp, error) {
	if len(expr) > 300 {
		return nil, fmt.Errorf("regex is too long")
	}
	if regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*{]`).MatchString(expr) {
		return nil, fmt.Errorf("nested quantifiers are not allowed")
	}
	goExpr := jsNamedGroup.ReplaceAllString(expr, "(?P<$1>")
	re, err := regexp.Compile(goExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid regex")
	}
	names := 0
	for _, n := range re.SubexpNames() {
		if n != "" {
			names++
		}
	}
	if names != 1 {
		return nil, fmt.Errorf("regex must contain exactly one named group")
	}
	return re, nil
}

var jsNamedGroup = regexp.MustCompile(`\(\?<([A-Za-z_][A-Za-z0-9_]*)>`)

// CompileRegexp exposes the one-named-group compiler to the parser.
func CompileRegexp(expr string) (*regexp.Regexp, error) {
	return compileUserRegexp(expr)
}

// ColumnNames is field order followed by system columns.
func (r Rule) ColumnNames() []string {
	out := make([]string, 0, len(r.Fields)+len(SystemColumns))
	for _, f := range r.Fields {
		out = append(out, f.Name)
	}
	out = append(out, SystemColumns...)
	return out
}

// CreateTableSQL builds a safe CREATE TABLE IF NOT EXISTS statement.
func CreateTableSQL(r Rule, dialect string) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(r.Table)
	b.WriteString(" (")
	for i, f := range r.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteString(" ")
		b.WriteString(sqlType(f.Type, dialect))
	}
	for _, col := range SystemColumns {
		b.WriteString(", ")
		b.WriteString(col)
		b.WriteString(" ")
		if col == "_rule_version" {
			b.WriteString("INTEGER")
		} else {
			b.WriteString("TEXT")
		}
	}
	b.WriteString(")")
	return b.String(), nil
}

func sqlType(fieldType, dialect string) string {
	switch fieldType {
	case "number":
		if dialect == DialectSQLite {
			return "REAL"
		}
		return "DOUBLE PRECISION"
	case "boolean":
		if dialect == DialectSQLite {
			return "INTEGER"
		}
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}
