package scheduling

import (
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/url"
	"sort"
	"strings"
	"time"
)

func orgCurrent(active, want string) string {
	if active == want {
		return "page"
	}
	return "false"
}
func optionalDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return date(t)
}
func displayDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02.01.2006")
}
func editionAction(id string) string {
	if id == "" {
		return "/admin/editions"
	}
	return "/admin/editions/" + id
}
func standAction(id string) string {
	if id == "" {
		return "/admin/stands"
	}
	return "/admin/stands/" + id
}
func participationPath(edition, id string) string {
	return "/admin/editions/" + edition + "/participations/" + id
}
func orgPrevious(path string, q url.Values, page int) string {
	prev, _ := pageLinks(path, q, page, false)
	return prev
}
func orgNext(path string, q url.Values, page int, more bool) string {
	_, next := pageLinks(path, q, page, more)
	return next
}
func accountLabel(p Person) string { return p.Name + " · Konto " + shortID(p.ID) }
func contactLabel(email, phone string) string {
	if email == "" && phone == "" {
		return "Keine Kontaktdaten hinterlegt"
	}
	if email == "" {
		return phone
	}
	if phone == "" {
		return email
	}
	return email + " · " + phone
}
func orgContext(path string, shellEdition string) string {
	q := url.Values{}
	if shellEdition != "" {
		q.Set("edition", shellEdition)
	}
	return canonical(path, q)
}
func sortedQueryKeys(q url.Values) []string {
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func directoryStandID(v DirectoryPageModel) string { return nonempty(v.MemberStand.ID, v.StandID) }
func membershipQuery(v MembershipPage) url.Values {
	q := contextQuery(v.Shell)
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	return q
}
func mergeQuery(v MergePage) url.Values {
	q := contextQuery(v.Shell)
	if v.TargetID != "" {
		q.Set("target", v.TargetID)
	}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	return q
}
func mergeCandidateURL(v MergePage, id string) string {
	q := mergeQuery(v)
	q.Del("q")
	if v.TargetID == "" {
		q.Set("target", id)
	} else {
		q.Set("source", id)
	}
	return canonical("/admin/workers/merge", q)
}
func capabilitiesQuery(v CapabilitiesPage) url.Values {
	q := contextQuery(v.Shell)
	if v.Kind != "" {
		q.Set("scope", v.Kind)
	}
	if v.Status != "" {
		q.Set("status", v.Status)
	}
	return q
}
func orgPageURL(path string, shell ui.ShellModel) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "admin" && parts[1] == "editions" && parts[2] != "new" {
		q.Set("edition", parts[2])
	}
	for key, values := range contextQuery(shell) {
		if q.Get(key) == "" {
			q[key] = values
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
