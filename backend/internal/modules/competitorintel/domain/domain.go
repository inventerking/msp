// Package domain holds Competitor Intelligence rules that need no database: robots.txt, page text,
// evidence labels and change detection.
package domain

import (
	"bufio"
	"bytes"
	"html"
	"regexp"
	"slices"
	"strings"
)

// Evidence classes, from most to least certain.
const (
	VerifiedPublic = "verified_public"
	Observed       = "observed"
	Inferred       = "inferred"
	AIEstimate     = "ai_estimate"
)

// UserAgent is how research identifies itself, and the robots.txt group it obeys.
const UserAgent = "MSPGrowthEngineResearch"

// Robots is a parsed robots.txt, for one user agent.
type Robots struct {
	rules []rule
}

type rule struct {
	allow bool
	path  string
}

// ParseRobots reads the rules for our agent, or for * when no group names us. An unreadable file
// allows everything, as the robots convention says.
func ParseRobots(body []byte) Robots {
	type group struct {
		agents []string
		rules  []rule
	}
	var groups []group
	var cur *group
	lastWasAgent := false
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if !lastWasAgent || cur == nil {
				groups = append(groups, group{})
				cur = &groups[len(groups)-1]
			}
			cur.agents = append(cur.agents, strings.ToLower(value))
			lastWasAgent = true
			continue
		case "allow", "disallow":
			if cur != nil {
				if value == "" && key == "disallow" {
					break // An empty Disallow allows everything.
				}
				cur.rules = append(cur.rules, rule{allow: key == "allow", path: value})
			}
		}
		lastWasAgent = false
	}
	var star *group
	for i := range groups {
		for _, a := range groups[i].agents {
			if strings.Contains(strings.ToLower(UserAgent), a) && a != "*" {
				return Robots{rules: groups[i].rules}
			}
			if a == "*" && star == nil {
				star = &groups[i]
			}
		}
	}
	if star != nil {
		return Robots{rules: star.rules}
	}
	return Robots{}
}

// Allowed applies the longest matching rule. On a tie, Allow wins.
func (r Robots) Allowed(path string) bool {
	if path == "" {
		path = "/"
	}
	best, allowed := -1, true
	for _, ru := range r.rules {
		if matches(ru.path, path) && (len(ru.path) > best || len(ru.path) == best && ru.allow) {
			best, allowed = len(ru.path), ru.allow
		}
	}
	return allowed
}

func matches(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, p := range parts {
		idx := strings.Index(path[pos:], p)
		if idx < 0 || i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(p)
	}
	return !anchored || pos == len(path) || strings.HasSuffix(pattern, "*")
}

var (
	dropBlocks = regexp.MustCompile(`(?is)<(script|style|noscript|template|svg)\b.*?</(script|style|noscript|template|svg)\s*>`)
	comments   = regexp.MustCompile(`(?s)<!--.*?-->`)
	blockTags  = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|h[1-6]|tr|td|th|section|article|header|footer|table)\b[^>]*>`)
	anyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	spaces     = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n\s*\n+`)
)

// MaxPageText bounds what is kept from one page and sent for analysis.
const MaxPageText = 20000

// PageText turns HTML into plain text, line by line. Scripts and styles are dropped, so nothing in
// them is analyzed, and the text is only ever displayed escaped.
func PageText(body []byte, contentType string) string {
	s := string(body)
	if strings.Contains(contentType, "html") || strings.Contains(strings.ToLower(s[:min(len(s), 512)]), "<html") {
		s = dropBlocks.ReplaceAllString(s, " ")
		s = comments.ReplaceAllString(s, " ")
		s = blockTags.ReplaceAllString(s, "\n")
		s = anyTag.ReplaceAllString(s, " ")
		s = html.UnescapeString(s)
	}
	s = spaces.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	s = strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n"))
	if len(s) > MaxPageText {
		s = s[:MaxPageText]
	}
	return s
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// Label decides how certain an extracted fact is. A model's claim counts as observed only when its
// quote is really on the page and contains the value. Anything else is an AI estimate, and no
// automated process ever upgrades an estimate.
func Label(page, quote, value string) string {
	q, v := normalize(quote), normalize(value)
	if q == "" || v == "" || !strings.Contains(normalize(page), q) || !strings.Contains(q, v) {
		return AIEstimate
	}
	return Observed
}

// Change is a difference in one attribute between what was known and what was just observed.
type Change struct {
	Attribute string
	Before    []string
	After     []string
}

// Changes compares value sets per attribute. Only attributes seen this time are compared: a page
// that no longer mentions pricing is not evidence that pricing was removed.
func Changes(before, after map[string][]string) []Change {
	var out []Change
	keys := make([]string, 0, len(after))
	for k := range after {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		a, b := set(after[k]), set(before[k])
		if !slices.Equal(a, b) {
			out = append(out, Change{Attribute: k, Before: b, After: a})
		}
	}
	return out
}

func set(v []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range v {
		n := normalize(x)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// Summary describes a change in words, for the change feed.
func (c Change) Summary() string {
	if len(c.Before) == 0 {
		return c.Attribute + " first seen: " + strings.Join(c.After, ", ")
	}
	return c.Attribute + " changed from " + strings.Join(c.Before, ", ") + " to " + strings.Join(c.After, ", ")
}
