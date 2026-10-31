package scheduling

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/tjrasche/HerbsfestOS/internal/auth"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type Handler struct{ store *Store }

func NewHandler(s *Store) *Handler { return &Handler{s} }
func (h *Handler) Register(private, public *http.ServeMux, optional func(http.Handler) http.Handler) {
	private.HandleFunc("GET /{$}", h.home)
	private.HandleFunc("GET /admin", h.home)
	private.HandleFunc("GET /feedback-notes", h.home)
	private.HandleFunc("POST /feedback-notes", h.feedback)
	private.HandleFunc("POST /edition-selection", h.selection)
	private.HandleFunc("POST /actions", h.action)
	private.HandleFunc("POST /requests", h.memberRequest)
	private.HandleFunc("GET /directory", h.directory)
	private.HandleFunc("GET /workers/{id}", h.worker)
	private.HandleFunc("GET /print/{id}", h.print)
	private.HandleFunc("POST /proof/{token}", h.redeem)
	public.Handle("GET /register/{token}", optional(http.HandlerFunc(h.signup)))
	public.Handle("POST /register/{token}", optional(http.HandlerFunc(h.signup)))
	public.HandleFunc("GET /proof/{token}", h.proof)
}
func identity(r *http.Request) Identity {
	u := auth.CurrentUser(r.Context())
	return Identity{u.Issuer, u.Subject}
}
func selected(r *http.Request) string {
	if c, e := r.Cookie("herbstfest_edition"); e == nil {
		return c.Value
	}
	return ""
}
func failure(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Speichern derzeit nicht möglich. Bitte erneut versuchen."
	switch {
	case errors.Is(err, ErrDenied):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, ErrUnavailable):
		status = http.StatusServiceUnavailable
		message = err.Error()
		w.Header().Set("Retry-After", "5")
	case errors.Is(err, ErrInvalid):
		status = http.StatusUnprocessableEntity
		message = err.Error()
	case errors.Is(err, ErrConflict):
		status = http.StatusConflict
		message = err.Error()
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}
func parse(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		return ErrInvalid
	}
	return nil
}
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	templ.Handler(c).ServeHTTP(w, r)
}
func (h *Handler) page(r *http.Request) (View, *http.Request, error) {
	v, err := h.store.View(r.Context(), identity(r), selected(r))
	if err != nil {
		return v, r, err
	}
	organizer := v.Global
	for _, p := range v.Schedules {
		organizer = organizer || p.CanEdit || p.CanManageMembers
	}
	return v, r.WithContext(ui.WithNavigation(r.Context(), ui.Navigation{Private: true, Global: v.Global, Organizer: organizer})), nil
}
func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	v, r, err := h.page(r)
	if err != nil {
		failure(w, err)
		return
	}
	if r.URL.Path == "/admin" {
		if !v.Global {
			failure(w, ErrDenied)
			return
		}
		render(w, r, AdminPage(v))
		return
	}
	if r.URL.Path == "/feedback-notes" {
		if !v.Global {
			failure(w, ErrDenied)
			return
		}
		render(w, r, FeedbackPage(v))
		return
	}
	render(w, r, SchedulePage(v, ""))
}
func (h *Handler) selection(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		failure(w, err)
		return
	}
	id := r.PostForm.Get("edition")
	if _, err := h.store.View(r.Context(), identity(r), id); err != nil {
		failure(w, err)
		return
	}
	cookie := &http.Cookie{Name: "herbstfest_edition", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode}
	if id == "" {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func formCommand(r *http.Request) Command {
	f := r.PostForm
	capacity, _ := strconv.Atoi(f.Get("capacity"))
	version, _ := strconv.Atoi(f.Get("version"))
	return Command{Action: f.Get("action"), ID: f.Get("id"), ParticipationID: f.Get("participation_id"), EditionID: f.Get("edition_id"), SourceID: f.Get("source_id"), StandID: f.Get("stand_id"), WorkerID: f.Get("worker_id"), OtherWorkerID: f.Get("other_worker_id"), AccountID: f.Get("account_id"), Label: f.Get("label"), StartDay: f.Get("start_day"), EndDay: f.Get("end_day"), Name: f.Get("name"), Title: f.Get("title"), Start: moment(f.Get("start"), f.Get("start_offset")), End: moment(f.Get("end"), f.Get("end_offset")), FirstName: f.Get("first_name"), LastName: f.Get("last_name"), Email: f.Get("email"), Phone: f.Get("phone"), Notes: f.Get("notes"), Token: f.Get("token"), Scope: f.Get("scope"), Capacity: capacity, Version: version, Enabled: f.Get("enabled") == "true", Confirm: f.Get("confirm") == "true"}
}
func (h *Handler) action(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		failure(w, err)
		return
	}
	out, err := h.store.Execute(r.Context(), identity(r), formCommand(r))
	if err != nil {
		failure(w, err)
		return
	}
	if out.Token != "" {
		path := "/proof/"
		if r.PostForm.Get("action") == "signup-link" {
			path = "/register/"
		}
		render(w, r, LinkPage(path+out.Token))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (h *Handler) feedback(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		failure(w, err)
		return
	}
	if err := h.store.AddFeedback(r.Context(), identity(r), selected(r), r.PostForm.Get("title")); err != nil {
		failure(w, err)
		return
	}
	http.Redirect(w, r, "/feedback-notes", http.StatusSeeOther)
}
func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.Directory(r.Context(), identity(r), r.URL.Query().Get("q"))
	if err != nil {
		failure(w, err)
		return
	}
	v, r, err := h.page(r)
	if err != nil {
		failure(w, err)
		return
	}
	render(w, r, DirectoryPage(v, rows, r.URL.Query().Get("q")))
}
func (h *Handler) worker(w http.ResponseWriter, r *http.Request) {
	worker, err := h.store.Worker(r.Context(), identity(r), r.PathValue("id"))
	if err != nil {
		failure(w, err)
		return
	}
	v, r, err := h.page(r)
	if err != nil {
		failure(w, err)
		return
	}
	render(w, r, WorkerPage(v, worker))
}
func (h *Handler) print(w http.ResponseWriter, r *http.Request) {
	v, r, err := h.page(r)
	if err != nil {
		failure(w, err)
		return
	}
	for _, p := range v.Schedules {
		if p.ID == r.PathValue("id") {
			render(w, r, PrintPage(p, h.store.Now()))
			return
		}
	}
	failure(w, ErrDenied)
}
func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	in := SignupInput{Token: token, Key: ID() + ID()}
	message := ""
	if r.Method == http.MethodPost {
		if err := parse(w, r); err != nil {
			failure(w, err)
			return
		}
		in.Key = r.PostForm.Get("key")
		in.FirstName = r.PostForm.Get("first_name")
		in.LastName = r.PostForm.Get("last_name")
		in.ShiftIDs = r.PostForm["shift"]
		out, err := h.store.Submit(r.Context(), identity(r), in)
		if err == nil {
			render(w, r, SubmittedPage(out.ConnectionToken))
			return
		}
		if errors.Is(err, ErrUnavailable) {
			failure(w, err)
			return
		}
		slog.Warn("signup failed", "error", err)
		message = "Nichts gespeichert. Bitte Auswahl prüfen; ein Dienst nimmt möglicherweise keine Anfragen mehr an. Deine Eingaben bleiben erhalten."
	}
	v, err := h.store.Signup(r.Context(), token)
	if err != nil {
		failure(w, err)
		return
	}
	render(w, r, SignupPage(v, in, message, h.store.Now()))
}
func (h *Handler) memberRequest(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		failure(w, err)
		return
	}
	_, err := h.store.Submit(r.Context(), identity(r), SignupInput{Key: r.PostForm.Get("key"), FirstName: r.PostForm.Get("first_name"), LastName: r.PostForm.Get("last_name"), ShiftIDs: r.PostForm["shift"]})
	if err != nil {
		failure(w, err)
		return
	}
	http.Redirect(w, r, "/#mine", http.StatusSeeOther)
}
func (h *Handler) proof(w http.ResponseWriter, r *http.Request) {
	kind, err := h.store.InspectProof(r.Context(), r.PathValue("token"))
	if err != nil {
		failure(w, err)
		return
	}
	render(w, r, ProofPage(kind, r.PathValue("token")))
}
func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		failure(w, err)
		return
	}
	action := "connect"
	if r.PostForm.Get("kind") == "organizer" {
		action = "redeem-organizer"
	}
	_, err := h.store.Execute(r.Context(), identity(r), Command{Action: action, Token: r.PathValue("token")})
	if err != nil {
		failure(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

func notesEditable(v View, id string) bool {
	if v.Global {
		return true
	}
	for _, p := range v.Schedules {
		if p.CanManageMembers {
			for _, m := range p.Members {
				if m.ID == id {
					return true
				}
			}
		}
	}
	return false
}
