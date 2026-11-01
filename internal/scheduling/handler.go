package scheduling

import (
	"github.com/a-h/templ"
	"github.com/tjrasche/HerbsfestOS/internal/auth"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	store   *Store
	private *http.ServeMux
	baseURL string
}

func NewHandler(s *Store, baseURL ...string) *Handler {
	h := &Handler{store: s}
	if len(baseURL) > 0 {
		h.baseURL = strings.TrimRight(baseURL[0], "/")
	}
	return h
}
func identity(r *http.Request) Identity {
	u := auth.CurrentUser(r.Context())
	return Identity{u.Issuer, u.Subject}
}
func parse(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		return ErrInvalid
	}
	return nil
}
func formCommand(r *http.Request) Command {
	f := r.PostForm
	capacity, _ := strconv.Atoi(f.Get("capacity"))
	version, _ := strconv.Atoi(f.Get("version"))
	c := Command{Action: f.Get("action"), ID: f.Get("id"), ParticipationID: f.Get("participation_id"), EditionID: f.Get("edition_id"), SourceID: f.Get("source_id"), StandID: f.Get("stand_id"), WorkerID: f.Get("worker_id"), OtherWorkerID: f.Get("other_worker_id"), AccountID: f.Get("account_id"), Label: f.Get("label"), StartDay: f.Get("start_day"), EndDay: f.Get("end_day"), Name: f.Get("name"), Title: f.Get("title"), Start: moment(f.Get("start"), f.Get("start_offset")), End: moment(f.Get("end"), f.Get("end_offset")), FirstName: f.Get("first_name"), LastName: f.Get("last_name"), Email: f.Get("email"), Phone: f.Get("phone"), Notes: f.Get("notes"), Token: f.Get("token"), Scope: f.Get("scope"), Capacity: capacity, Version: version, Enabled: f.Get("enabled") == "true", Confirm: f.Get("confirm") == "true"}
	c.CapabilityID = f.Get("capability_id")
	c.PreviewFingerprint = f.Get("fingerprint")
	c.EmailChoice = f.Get("email_choice")
	c.PhoneChoice = f.Get("phone_choice")
	c.ClearEmail = f.Get("clear_email") == "true"
	c.ClearPhone = f.Get("clear_phone") == "true"
	c.ClearNotes = f.Get("clear_notes") == "true"
	_, notesPresent := f["notes"]
	c.PreserveNotes = !notesPresent
	return c
}

// action remains a compatibility adapter for old no-store forms. The live UI
// uses resource-owned routes and the same authoritative Execute boundary.
func (h *Handler) action(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	c := formCommand(r)
	switch c.Action {
	case "signup-link", "connection-link", "capability-reissue", "capability-revoke", "team-remove", "global-remove", "member-remove", "unassign":
		if !c.Confirm {
			h.problem(w, r, ErrInvalid)
			return
		}
	case "participation":
		if !c.Enabled && !c.Confirm {
			h.problem(w, r, ErrInvalid)
			return
		}
	}
	switch c.Action {
	case "signup-link", "connection-link", "organizer-invite", "capability-reissue":
		h.issuePresentedLink(w, r, c, "/")
		return
	}
	out, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	fallback := "/"
	switch c.Action {
	case "worker-save":
		fallback = "/workers/" + c.ID
	case "edition-save":
		fallback = "/admin/editions/" + out.ID
	case "stand-save":
		fallback = "/admin/stands/" + out.ID
	case "activate":
		fallback = "/admin/editions/" + c.ID
	}
	h.success(w, r, withNotice(h.returnTo(r, r.PostForm.Get("return_to"), fallback), "saved"))
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func n(i int) string                { return strconv.Itoa(i) }
func u(i uint64) string             { return strconv.FormatUint(i, 10) }
func iso(t time.Time) string        { return t.In(Berlin).Format(time.RFC3339) }
func date(t time.Time) string       { return t.Format("2006-01-02") }
func loginPath(token string) string { return "/auth/login?next=" + url.QueryEscape("/proof/"+token) }
func freshKey() string              { return ID() + ID() }
func safe(s string) templ.SafeURL   { return templ.SafeURL(s) }
func status(s string) string {
	switch s {
	case "pending":
		return "Offen"
	case "approved":
		return "Bestätigt"
	case "declined":
		return "Abgelehnt"
	case "withdrawn":
		return "Zurückgezogen"
	case "cancelled":
		return "Abgesagt"
	}
	return strings.TrimSpace(s)
}
func timeInput(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return iso(t)
}
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
func toggleLabel(enabled bool) string {
	if enabled {
		return "Teilnahme deaktivieren"
	}
	return "Teilnahme reaktivieren"
}
func proofLabel(kind string) string {
	if kind == "organizer" {
		return "Organisatorrolle für mein Konto annehmen"
	}
	return "Helferdatensatz mit meinem Konto verbinden"
}

func moment(value, offset string) string {
	if len(value) == 16 && (offset == "+01:00" || offset == "+02:00") {
		return value + ":00" + offset
	}
	return value
}
func localInput(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Berlin).Format("2006-01-02T15:04")
}
func summer(t time.Time) bool { return t.IsZero() || t.In(Berlin).IsDST() }
