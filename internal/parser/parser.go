package parser

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"

	"responseforge/internal/pattern"
	"responseforge/internal/rule"
)

// Parse applies a declarative rule. Rows contain only user fields.
func Parse(r rule.Rule, contentType, body string) ([]map[string]any, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var (
		rows []map[string]any
		err  error
	)
	switch pattern.Kind(contentType) {
	case "json":
		rows, err = parseJSON(r, body)
	case "html":
		rows, err = parseHTML(r, body)
	default:
		rows, err = parseText(r, body)
	}
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func parseJSON(r rule.Rule, body string) ([]map[string]any, error) {
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("invalid json")
	}
	var records []any
	if r.RecordPath == "" {
		records = []any{root}
	} else {
		vals, exists, err := evalJSONPath(root, r.RecordPath)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("record_path matched nothing")
		}
		vals = flattenRecordValues(vals)
		if len(vals) == 0 {
			return []map[string]any{}, nil
		}
		records = vals
	}
	return project(r, len(records), func(i int, f rule.Field) (any, bool, error) {
		return lookupJSONField(root, records[i], r.RecordPath, f.From, i, len(records))
	})
}

func parseHTML(r rule.Rule, body string) ([]map[string]any, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("invalid html")
	}
	if r.RecordPath != "" {
		if _, err := cascadia.Compile(r.RecordPath); err != nil {
			return nil, fmt.Errorf("invalid record_path")
		}
	}
	var nodes []*goquery.Selection
	if r.RecordPath == "" {
		nodes = []*goquery.Selection{doc.Selection}
	} else {
		found := doc.Find(r.RecordPath)
		if found.Length() == 0 {
			return nil, fmt.Errorf("record_path matched nothing")
		}
		found.Each(func(_ int, s *goquery.Selection) {
			nodes = append(nodes, s)
		})
	}
	return project(r, len(nodes), func(i int, f rule.Field) (any, bool, error) {
		if strings.HasPrefix(f.From, "$") || strings.Contains(f.From, "(?P<") || strings.Contains(f.From, "(?<") {
			return nil, false, fmt.Errorf("html fields use css selectors")
		}
		if _, err := cascadia.Compile(f.From); err != nil {
			return nil, false, fmt.Errorf("invalid selector")
		}
		scope := nodes[i]
		sel := scope.Find(f.From)
		if sel.Length() == 0 && scope.Is(f.From) {
			sel = scope
		}
		if sel.Length() == 0 {
			return nil, false, nil
		}
		node := sel.First()
		if f.Attr != "" {
			v, ok := node.Attr(f.Attr)
			if !ok {
				return nil, false, nil
			}
			return v, true, nil
		}
		return strings.TrimSpace(node.Text()), true, nil
	})
}

func parseText(r rule.Rule, body string) ([]map[string]any, error) {
	chunks := []string{body}
	if r.RecordPath != "" {
		if strings.HasPrefix(r.RecordPath, "$") {
			return nil, fmt.Errorf("text rules cannot use jsonpath record_path")
		}
		re, err := rule.CompileRegexp(looseRecordRegexp(r.RecordPath))
		if err != nil {
			// A record splitter may omit a named group. Compile it as a plain RE2 expression.
			re, err = regexp.Compile(r.RecordPath)
			if err != nil {
				return nil, fmt.Errorf("invalid record_path")
			}
		}
		found := re.FindAllString(body, -1)
		if len(found) == 0 {
			return nil, fmt.Errorf("record_path matched nothing")
		}
		chunks = found
	}
	return project(r, len(chunks), func(i int, f rule.Field) (any, bool, error) {
		re, err := rule.CompileRegexp(f.From)
		if err != nil {
			return nil, false, err
		}
		match := re.FindStringSubmatch(chunks[i])
		if match == nil {
			return nil, false, nil
		}
		for gi, name := range re.SubexpNames() {
			if name != "" && gi < len(match) {
				return match[gi], true, nil
			}
		}
		return nil, false, nil
	})
}

// looseRecordRegexp lets a record_path regex through when it already has one named group.
func looseRecordRegexp(expr string) string {
	return expr
}

type extractFunc func(i int, f rule.Field) (any, bool, error)

func project(r rule.Rule, n int, extract extractFunc) ([]map[string]any, error) {
	var rows []map[string]any
	var lastErr error
	for i := 0; i < n; i++ {
		row := map[string]any{}
		failed := false
		for _, f := range r.Fields {
			raw, found, err := extract(i, f)
			if err != nil {
				return nil, err
			}
			if !found {
				if f.Required {
					failed = true
					lastErr = fmt.Errorf("required field %s is missing", f.Name)
				}
				row[f.Name] = nil
				continue
			}
			coerced, ok := coerce(f.Type, raw)
			if !ok {
				// A present value that cannot be converted becomes null and does not fail the row.
				row[f.Name] = nil
				continue
			}
			if f.Required && blank(coerced) {
				failed = true
				lastErr = fmt.Errorf("required field %s is empty", f.Name)
				row[f.Name] = nil
				continue
			}
			row[f.Name] = coerced
		}
		if !failed {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("parser produced no rows")
	}
	return rows, nil
}

func blank(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

func coerce(fieldType string, raw any) (any, bool) {
	if raw == nil {
		return nil, false
	}
	switch fieldType {
	case "string":
		switch v := raw.(type) {
		case string:
			return v, true
		case json.Number:
			return v.String(), true
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, false
			}
			return string(b), true
		}
	case "number":
		switch v := raw.(type) {
		case float64:
			return v, true
		case json.Number:
			f, err := v.Float64()
			return f, err == nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return f, err == nil
		case bool:
			if v {
				return float64(1), true
			}
			return float64(0), true
		default:
			return nil, false
		}
	case "boolean":
		switch v := raw.(type) {
		case bool:
			return v, true
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			return b, err == nil
		case float64:
			return v != 0, true
		default:
			return nil, false
		}
	case "timestamp":
		switch v := raw.(type) {
		case float64:
			if v != math.Trunc(v) {
				return nil, false
			}
			return strconv.FormatFloat(v, 'f', 0, 64), true
		case json.Number:
			if _, err := v.Int64(); err != nil {
				return nil, false
			}
			return v.String(), true
		}
		s, ok := raw.(string)
		if !ok {
			s = fmt.Sprint(raw)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, false
		}
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			return s, true
		}
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return s, true
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return s, true
		}
		return nil, false
	default:
		return nil, false
	}
}

type segKind int

const (
	segKey segKind = iota
	segIndex
	segWild
)

type segment struct {
	kind  segKind
	key   string
	index int
}

func lookupJSONField(root, record any, recordPath, from string, index, count int) (any, bool, error) {
	pick, err := firstJSONValue(record, from)
	if err != nil || pick != nil {
		return pick, pick != nil, err
	}
	if rel := relativeToRecord(recordPath, from); rel != "" {
		pick, err = firstJSONValue(record, rel)
		if err != nil || pick != nil {
			return pick, pick != nil, err
		}
	}
	if recordPath == "" {
		return nil, false, nil
	}
	vals, exists, err := evalJSONPath(root, from)
	if err != nil || !exists || len(vals) == 0 {
		return nil, false, err
	}
	if len(vals) == count && index >= 0 && index < len(vals) && vals[index] != nil {
		return vals[index], true, nil
	}
	if count == 1 && vals[0] != nil {
		if len(vals) == 1 {
			return vals[0], true, nil
		}
		return vals, true, nil
	}
	return nil, false, nil
}

func firstJSONValue(record any, path string) (any, error) {
	vals, exists, err := evalJSONPath(record, path)
	if err != nil || !exists || len(vals) == 0 || vals[0] == nil {
		return nil, err
	}
	if len(vals) == 1 {
		return vals[0], nil
	}
	return vals, nil
}

func relativeToRecord(recordPath, from string) string {
	if recordPath == "" || from == "" {
		return ""
	}
	if strings.HasPrefix(from, recordPath+".") {
		return "$." + strings.TrimPrefix(from, recordPath+".")
	}
	base := strings.TrimSuffix(recordPath, "[*]")
	if base != recordPath && strings.HasPrefix(from, base+".") {
		rest := strings.TrimPrefix(from, base+".")
		rest = strings.TrimPrefix(rest, "[*].")
		if rest == "" || strings.HasPrefix(rest, "[") {
			return ""
		}
		return "$." + rest
	}
	return ""
}

func evalJSONPath(root any, path string) ([]any, bool, error) {
	if path == "" || !strings.HasPrefix(path, "$") {
		return nil, false, fmt.Errorf("jsonpath must start with $")
	}
	segs, err := parseJSONPath(path)
	if err != nil {
		return nil, false, err
	}
	vals, ok := walkJSON(root, segs)
	return vals, ok, nil
}

func walkJSON(item any, segs []segment) ([]any, bool) {
	if len(segs) == 0 {
		return []any{item}, true
	}
	sg := segs[0]
	switch sg.kind {
	case segKey:
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := obj[sg.key]
		if !ok {
			return nil, false
		}
		return walkJSON(v, segs[1:])
	case segIndex:
		arr, ok := asArray(item)
		if !ok || sg.index < 0 || sg.index >= len(arr) {
			return nil, false
		}
		return walkJSON(arr[sg.index], segs[1:])
	case segWild:
		arr, ok := asArray(item)
		if !ok {
			return nil, false
		}
		if len(segs) == 1 {
			return arr, true
		}
		var all []any
		exists := false
		for _, el := range arr {
			vals, ok := walkJSON(el, segs[1:])
			if ok {
				exists = true
				all = append(all, vals...)
			}
		}
		return all, exists
	default:
		return nil, false
	}
}

// flattenRecordValues turns a path that selected a whole array, such as $.comments,
// into one record per element. $.comments[*] already returns the elements.
func flattenRecordValues(vals []any) []any {
	if len(vals) == 1 {
		if arr, ok := vals[0].([]any); ok {
			return arr
		}
	}
	return vals
}

func asArray(v any) ([]any, bool) {
	arr, ok := v.([]any)
	return arr, ok
}

func parseJSONPath(path string) ([]segment, error) {
	if path == "$" {
		return nil, nil
	}
	rest := path[1:]
	var segs []segment
	for len(rest) > 0 {
		switch rest[0] {
		case '.':
			rest = rest[1:]
			if rest == "" || rest[0] == '.' || rest[0] == '[' {
				return nil, fmt.Errorf("invalid jsonpath")
			}
			i := 0
			for i < len(rest) && (isIdent(rest[i])) {
				i++
			}
			if i == 0 {
				return nil, fmt.Errorf("invalid jsonpath")
			}
			segs = append(segs, segment{kind: segKey, key: rest[:i]})
			rest = rest[i:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("invalid jsonpath")
			}
			inner := rest[1:end]
			switch {
			case inner == "*":
				segs = append(segs, segment{kind: segWild})
			case len(inner) >= 2 && (inner[0] == '\'' || inner[0] == '"'):
				q := inner[0]
				if inner[len(inner)-1] != q {
					return nil, fmt.Errorf("invalid jsonpath")
				}
				segs = append(segs, segment{kind: segKey, key: inner[1 : len(inner)-1]})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid jsonpath")
				}
				segs = append(segs, segment{kind: segIndex, index: n})
			}
			rest = rest[end+1:]
		default:
			return nil, fmt.Errorf("invalid jsonpath")
		}
	}
	return segs, nil
}

func isIdent(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
