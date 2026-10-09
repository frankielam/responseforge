package pattern

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Match reports whether pattern (with * wildcards) matches the full URL.
func Match(pattern, rawURL string) bool {
	if pattern == "" || strings.ContainsAny(pattern, "\r\n") {
		return false
	}
	re, err := compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(rawURL)
}

func compile(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(p))
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// LiteralPrefix is the length of the pattern before the first *.
func LiteralPrefix(pattern string) int {
	i := strings.IndexByte(pattern, '*')
	if i < 0 {
		return len(pattern)
	}
	return i
}

// LiteralCount is the number of non-wildcard characters.
func LiteralCount(pattern string) int {
	return len(pattern) - strings.Count(pattern, "*")
}

// Candidate is a saved rule considered for matching.
type Candidate struct {
	Pattern      string
	ContentTypes []string
	UpdatedAt    time.Time
}

// Select returns the index of the winning rule.
// Longer literal prefixes win, then more literal characters, then the newest update.
func Select(rules []Candidate, rawURL, contentType string) (int, bool) {
	best := -1
	for i, r := range rules {
		if !Match(r.Pattern, rawURL) || !contentTypeAllowed(r.ContentTypes, contentType) {
			continue
		}
		if best < 0 || moreSpecific(r, rules[best]) {
			best = i
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

func moreSpecific(a, b Candidate) bool {
	ap, bp := LiteralPrefix(a.Pattern), LiteralPrefix(b.Pattern)
	if ap != bp {
		return ap > bp
	}
	al, bl := LiteralCount(a.Pattern), LiteralCount(b.Pattern)
	if al != bl {
		return al > bl
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func contentTypeAllowed(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	actual = baseType(actual)
	if actual == "" {
		return false
	}
	for _, item := range allowed {
		item = baseType(item)
		if item == "" {
			continue
		}
		if item == actual || strings.Contains(actual, item) || strings.Contains(item, actual) {
			return true
		}
	}
	return false
}

func baseType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// Textual reports whether a content type may be stored.
func Textual(contentType string) bool {
	ct := baseType(contentType)
	switch {
	case ct == "":
		return false
	case strings.Contains(ct, "json"),
		strings.Contains(ct, "html"),
		strings.Contains(ct, "xml"),
		strings.Contains(ct, "csv"),
		strings.HasPrefix(ct, "text/"):
		return true
	default:
		return false
	}
}

// Kind is the declarative extractor family for a content type.
func Kind(contentType string) string {
	ct := baseType(contentType)
	switch {
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "html"):
		return "html"
	default:
		return "text"
	}
}

// Normalize turns a sample URL into a reusable pattern.
// Fragments and the query string are removed, because tokens, signatures,
// and ids there change on every request. Numeric or UUID path segments become *.
// A query string is covered by a trailing * so the next request still matches.
func Normalize(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("url must be http(s) with a host")
	}
	u.Fragment = ""
	u.User = nil
	parts := strings.Split(u.EscapedPath(), "/")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if allDigits(p) || uuidRE.MatchString(p) {
			parts[i] = "*"
		}
	}
	path := strings.Join(parts, "/")
	if u.RawQuery != "" && !strings.HasSuffix(path, "*") {
		path += "*"
	}
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.Host)
	b.WriteString(path)
	return collapseStars(b.String()), nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func collapseStars(s string) string {
	for strings.Contains(s, "**") {
		s = strings.ReplaceAll(s, "**", "*")
	}
	return s
}
