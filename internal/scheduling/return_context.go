package scheduling

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type ReturnContext struct {
	Path  string
	Query url.Values
}

var returnPath = regexp.MustCompile(`^/(?:$|requests/(?:mine|pending)$|directory$|feedback-notes$|admin(?:/(?:editions(?:/new|/[A-Za-z0-9_-]+(?:/participations/[A-Za-z0-9_-]+)?)?|stands(?:/new|/[A-Za-z0-9_-]+)?|access|invitations|workers/merge))?$|shifts/[A-Za-z0-9_-]+(?:/edit)?$|participations/[A-Za-z0-9_-]+/shifts/new$|workers/[A-Za-z0-9_-]+(?:/assign)?$|stands/[A-Za-z0-9_-]+/members$|links/[a-f0-9]{64}$)`)

func parseReturn(raw string) (ReturnContext, bool) {
	return parseReturnDepth(raw, 0)
}
func parseReturnDepth(raw string, depth int) (ReturnContext, bool) {
	if raw == "" || len(raw) > 1500 || strings.ContainsAny(raw, "\\\r\n") || strings.HasPrefix(raw, "//") {
		return ReturnContext{}, false
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return ReturnContext{}, false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || !returnPath.MatchString(u.Path) {
		return ReturnContext{}, false
	}
	q := url.Values{}
	for _, key := range []string{"edition", "stand", "member_stand", "day", "page", "q", "status", "target", "source", "shift"} {
		if value := u.Query().Get(key); value != "" {
			if len(value) > 200 || strings.ContainsAny(value, "\\\r\n") {
				return ReturnContext{}, false
			}
			for _, r := range value {
				if unicode.IsControl(r) {
					return ReturnContext{}, false
				}
			}
			q.Set(key, value)
		}
	}
	if scope := u.Query().Get("scope"); scope != "" {
		if scope != "team" && scope != "global" {
			return ReturnContext{}, false
		}
		q.Set("scope", scope)
	}
	if nested := u.Query().Get("return_to"); nested != "" {
		if depth >= 1 {
			return ReturnContext{}, false
		}
		next, ok := parseReturnDepth(nested, depth+1)
		if !ok {
			return ReturnContext{}, false
		}
		q.Set("return_to", next.URL())
	}
	if day := q.Get("day"); day != "" && !validDay(day) {
		return ReturnContext{}, false
	}
	if page := q.Get("page"); page != "" {
		q.Set("page", strconvPage(page))
	}
	switch notice := u.Query().Get("notice"); notice {
	case "saved", "created", "removed", "activated", "updated", "requested", "withdrawn", "decision", "cancelled":
		q.Set("notice", notice)
	}
	return ReturnContext{u.Path, q}, true
}
func (r ReturnContext) URL() string { return canonical(r.Path, r.Query) }
func safeReturn(raw, fallback string) string {
	if r, ok := parseReturn(raw); ok {
		return r.URL()
	}
	return fallback
}
func strconvPage(value string) string { return n(boundedPage(value)) }
func validDay(value string) bool      { _, err := time.Parse("2006-01-02", value); return err == nil }
