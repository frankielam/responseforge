package rule

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	createRE = regexp.MustCompile(`(?i)^CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+("?)([a-z][a-z0-9_]{0,62})"?\s*\((.*)\)\s*$`)
	bannedRE = regexp.MustCompile(`(?i)(^|[^A-Z0-9_])(DROP|ALTER|ATTACH|DETACH|PRAGMA|DELETE|INSERT|UPDATE|TRUNCATE|GRANT|REVOKE|VACUUM|COPY|CALL|EXECUTE|UNION|SELECT|INTO|FROM|WHERE|JOIN|FOREIGN|REFERENCES|TRIGGER|PROCEDURE|MERGE|REPLACE|LOAD|ANALYSE|ANALYZE)([^A-Z0-9_]|$)`)
)

// ValidateCreateTable allows one CREATE TABLE IF NOT EXISTS whose columns cover the rule.
func ValidateCreateTable(statement string, r Rule, dialect string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	s := strings.TrimSpace(statement)
	if s == "" {
		return fmt.Errorf("create_table is required")
	}
	if strings.Contains(s, "--") || strings.Contains(s, "/*") || strings.Contains(s, "*/") {
		return fmt.Errorf("sql comments are not allowed")
	}
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)
	if strings.Contains(s, ";") {
		return fmt.Errorf("multiple statements are not allowed")
	}
	if bannedRE.MatchString(strings.ToUpper(s)) {
		return fmt.Errorf("statement contains a forbidden keyword")
	}
	if dialect == DialectSQLite {
		upper := strings.ToUpper(s)
		for _, bad := range []string{"BIGSERIAL", "JSONB", "SERIAL", "NEXTVAL"} {
			if strings.Contains(upper, bad) {
				return fmt.Errorf("statement uses a non-sqlite type")
			}
		}
	}
	m := createRE.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("only a single CREATE TABLE IF NOT EXISTS is allowed")
	}
	if m[2] != r.Table {
		return fmt.Errorf("create_table name must match rule.table")
	}
	cols := columnNames(m[3])
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	for _, want := range r.ColumnNames() {
		if !have[want] {
			return fmt.Errorf("create_table is missing column %s", want)
		}
	}
	expected, err := CreateTableSQL(r, dialect)
	if err != nil {
		return err
	}
	if normalizeSQL(s) != normalizeSQL(expected) {
		return fmt.Errorf("create_table must match the server-generated schema")
	}
	return nil
}

func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func columnNames(body string) []string {
	var names []string
	depth := 0
	start := 0
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if name := firstIdent(body[start:i]); name != "" {
					names = append(names, name)
				}
				start = i + 1
			}
		}
	}
	if name := firstIdent(body[start:]); name != "" {
		names = append(names, name)
	}
	return names
}

func firstIdent(part string) string {
	s := strings.TrimSpace(part)
	if s == "" {
		return ""
	}
	if s[0] == '"' || s[0] == '`' {
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 {
			return ""
		}
		return strings.ToLower(s[1 : end+1])
	}
	i := 0
	for i < len(s) {
		r := rune(s[i])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			i++
			continue
		}
		break
	}
	return strings.ToLower(s[:i])
}
