package scheduling

import (
	"context"
	"errors"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (h *Handler) registerSchedule(private, public *http.ServeMux, optional func(http.Handler) http.Handler) {
	private.HandleFunc("GET /{$}", h.scheduleScreen)
	private.HandleFunc("GET /shifts/{shift}", h.shiftScreen)
	private.HandleFunc("GET /participations/{participation}/shifts/new", h.shiftNewScreen)
	private.HandleFunc("GET /shifts/{shift}/edit", h.shiftEditScreen)
	private.HandleFunc("POST /participations/{participation}/shifts", h.shiftSaveScreen)
	private.HandleFunc("POST /shifts/{shift}", h.shiftSaveScreen)
	private.HandleFunc("POST /shifts/{shift}/cancel", h.shiftCancelScreen)
	private.HandleFunc("POST /shifts/{shift}/assignments", h.shiftAssignScreen)
	private.HandleFunc("GET /shifts/{shift}/assignments/{worker}/remove", h.shiftRemoveScreen)
	private.HandleFunc("POST /shifts/{shift}/assignments/{worker}/remove", h.shiftUnassignScreen)
	private.HandleFunc("GET /requests/mine", h.ownRequestsScreen)
	private.HandleFunc("GET /requests/pending", h.pendingRequestsScreen)
	private.HandleFunc("POST /requests/{request}/approve", h.requestApproveScreen)
	private.HandleFunc("POST /requests/{request}/decline", h.requestDeclineScreen)
	private.HandleFunc("POST /requests/{request}/withdraw", h.requestWithdrawScreen)
	private.HandleFunc("POST /requests", h.memberSignupScreen)
	private.HandleFunc("GET /print/{id}", h.printScreen)
	private.HandleFunc("POST /proof/{token}", h.proofAcceptScreen)
	public.Handle("GET /register/{token}", optional(http.HandlerFunc(h.publicSignupScreen)))
	public.Handle("POST /register/{token}", optional(http.HandlerFunc(h.publicSignupScreen)))
	public.Handle("GET /proof/{token}", optional(http.HandlerFunc(h.proofScreen)))
}
func filterRequest(r *http.Request) ScheduleFilter {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		if c, ok := parseReturn(r.PostForm.Get("return_to")); ok {
			for _, key := range []string{"edition", "stand", "day", "page"} {
				if q.Get(key) == "" {
					q.Set(key, c.Query.Get(key))
				}
			}
		}
	}
	return ScheduleFilter{Edition: q.Get("edition"), Stand: q.Get("stand"), Day: q.Get("day"), Page: boundedPage(q.Get("page"))}
}
func scheduleMeta(heading string) ui.PageMeta {
	if heading == "Dienstplanung" {
		return ui.PageMeta{Title: "Dienstplanung · HerbstfestOS", Active: "schedule", Heading: heading, Breadcrumbs: []ui.Breadcrumb{{Label: heading}}}
	}
	return ui.PageMeta{Title: heading + " · HerbstfestOS", Active: "schedule", Heading: heading, Breadcrumbs: []ui.Breadcrumb{{Label: "Dienstplanung", Href: "/"}, {Label: heading}}}
}
func savedFeedback(r *http.Request) FormState {
	f := FormState{}
	switch r.URL.Query().Get("notice") {
	case "saved":
		f.Message = "Änderungen gespeichert."
	case "requested":
		f.Message = "Deine Anfrage ist gespeichert und wartet auf Bestätigung."
	case "withdrawn":
		f.Message = "Deine Anfrage wurde zurückgezogen."
	case "decision":
		f.Message = "Entscheidung gespeichert."
	case "cancelled":
		f.Message = "Der Dienst wurde abgesagt."
	}
	return f
}
func withNotice(destination, code string) string {
	u, err := url.Parse(destination)
	if err != nil {
		return destination
	}
	q := u.Query()
	q.Set("notice", code)
	u.RawQuery = q.Encode()
	return u.String()
}
func (h *Handler) scheduleScreen(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.ScheduleSummary(r.Context(), identity(r), filterRequest(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, scheduleMeta("Dienstplanung"), m.Shell, ScheduleContent(m, savedFeedback(r)))
}
func (h *Handler) shiftScreen(w http.ResponseWriter, r *http.Request) {
	h.renderShift(w, r, r.PathValue("shift"), savedFeedback(r), 200)
}
func (h *Handler) renderShift(w http.ResponseWriter, r *http.Request, id string, f FormState, status int) {
	m, err := h.store.ShiftDetail(r.Context(), identity(r), id, filterRequest(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, scheduleMeta(m.Shift.Title), m.Shell, ShiftDetailContent(m, f))
}

type ShiftFormModel struct {
	Shell                        ui.ShellModel
	Participation                Participation
	Edition                      Edition
	Shift                        Shift
	Form                         FormState
	StartAmbiguous, EndAmbiguous bool
	ReconciliationRequired       bool
	ReturnURL, Action            string
}

func shiftFormHeading(id string) string {
	if id == "" {
		return "Dienst anlegen"
	}
	return "Dienst bearbeiten"
}
func (s *Store) shiftForm(ctx context.Context, who Identity, id, part string, filter ScheduleFilter) (ShiftFormModel, error) {
	var out ShiftFormModel
	shell, err := s.readPage(ctx, who, filter.Edition, func(t *Tx, shell ui.ShellModel) error {
		var err error
		if id != "" {
			out.Shift, out.Participation, err = t.shift(id, false)
		} else {
			out.Participation, err = t.participation(part)
		}
		if err != nil {
			return err
		}
		if out.Participation.EditionID != shell.Selected {
			return ErrDenied
		}
		if err = t.permitPart(out.Participation, "edit_schedule"); err != nil {
			return err
		}
		if out.Shift.Cancelled {
			return ErrConflict
		}
		out.Edition, err = t.edition(out.Participation.EditionID)
		if err != nil {
			return err
		}
		filter.Stand = out.Participation.ID
		if shell.Global {
			filter.Edition = shell.Selected
		}
		if filter.Day == "" {
			if id != "" {
				filter.Day = out.Shift.StartsAt.In(Berlin).Format("2006-01-02")
			} else {
				filter.Day = date(out.Edition.StartDay)
			}
		}
		if !validDay(filter.Day) {
			return ErrInvalid
		}
		if id == "" {
			start, _ := time.ParseInLocation("2006-01-02", filter.Day, Berlin)
			out.Shift.StartsAt = start.Add(9 * time.Hour)
			out.Shift.EndsAt = start.Add(12 * time.Hour)
			out.Shift.Capacity = 1
		}
		out.ReturnURL = scheduleURL(filter)
		out.Action = "/participations/" + out.Participation.ID + "/shifts"
		if id != "" {
			out.Action = "/shifts/" + id
		}
		return nil
	})
	out.Shell = shell
	return out, err
}
func (h *Handler) shiftNewScreen(w http.ResponseWriter, r *http.Request) {
	h.renderShiftForm(w, r, "", r.PathValue("participation"), FormState{}, false, false, 200)
}
func (h *Handler) shiftEditScreen(w http.ResponseWriter, r *http.Request) {
	h.renderShiftForm(w, r, r.PathValue("shift"), "", FormState{}, false, false, 200)
}
func (h *Handler) renderShiftForm(w http.ResponseWriter, r *http.Request, id, part string, f FormState, startAmbiguous, endAmbiguous bool, status int) {
	m, err := h.store.shiftForm(r.Context(), identity(r), id, part, filterRequest(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	m.Form = f
	m.Form.ID = "shift"
	m.StartAmbiguous = startAmbiguous
	m.EndAmbiguous = endAmbiguous
	if r.Method == http.MethodPost {
		m.ReturnURL = h.returnTo(r, r.PostForm.Get("return_to"), m.ReturnURL)
		version, _ := strconv.Atoi(r.PostForm.Get("version"))
		m.ReconciliationRequired = id != "" && (version != m.Shift.Version || r.PostForm.Has("reconcile_version"))
		if m.ReconciliationRequired && status == http.StatusConflict {
			m.Form.Summary = "Der Dienst wurde inzwischen geändert. Deine Eingaben bleiben erhalten. Bitte mit den aktuell gespeicherten Werten abgleichen und erneut bestätigen."
		}
	}
	h.respond(w, r, status, scheduleMeta(shiftFormHeading(id)), m.Shell, ShiftFormContent(m))
}
func (h *Handler) shiftSaveScreen(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	f := formState(r, nil)
	f.ID = "shift"
	capacity, e := strconv.Atoi(r.PostForm.Get("capacity"))
	if e != nil || capacity < 1 {
		f.Errors["capacity"] = "Bitte mindestens einen Platz angeben."
	}
	if !textValid(r.PostForm.Get("title"), 120) {
		f.Errors["title"] = "Bitte eine Dienstbezeichnung mit höchstens 120 Zeichen angeben."
	}
	start, ambiguousStart, startErr := berlinMoment(r.PostForm.Get("start"), r.PostForm.Get("start_offset"))
	end, ambiguousEnd, endErr := berlinMoment(r.PostForm.Get("end"), r.PostForm.Get("end_offset"))
	if startErr != nil {
		f.Errors["start"] = errorMessage(startErr)
	}
	if endErr != nil {
		f.Errors["end"] = errorMessage(endErr)
	}
	id, part := r.PathValue("shift"), r.PathValue("participation")
	if len(f.Errors) > 0 {
		f.Summary = "Bitte die markierten Felder prüfen. Deine Eingaben bleiben erhalten."
		h.renderShiftForm(w, r, id, part, f, ambiguousStart, ambiguousEnd, 422)
		return
	}
	c := formCommand(r)
	c.Action = "shift-save"
	c.ID = id
	c.ParticipationID = part
	c.Start = start
	c.End = end
	c.Capacity = capacity
	if id != "" && r.PostForm.Has("reconcile_version") {
		version, err := strconv.Atoi(r.PostForm.Get("reconcile_version"))
		if err != nil || version < 1 || r.PostForm.Get("reconcile_confirm") != "true" {
			f.Errors["reconcile_confirm"] = "Bitte die aktuell gespeicherten Werte prüfen und den Abgleich ausdrücklich bestätigen."
			h.renderShiftForm(w, r, id, part, f, ambiguousStart, ambiguousEnd, http.StatusConflict)
			return
		}
		// The original edit revision stays in the form. Only an explicit
		// acknowledgement may retry against the revision shown for comparison;
		// Execute still checks that revision under its write lock.
		c.Version = version
	}
	out, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
			f.Summary = errorMessage(err)
			if errors.Is(err, ErrInvalid) {
				f.Errors["end"] = "Bitte Reihenfolge und zulässigen Zeitraum der Ausgabe prüfen."
			}
			h.renderShiftForm(w, r, id, part, f, ambiguousStart, ambiguousEnd, errorStatus(err))
			return
		}
		h.problem(w, r, err)
		return
	}
	q := filterValues(filterRequest(r))
	q.Del("page")
	destination := canonical("/shifts/"+out.ID, q)
	h.success(w, r, withNotice(destination, "saved"))
}
func (h *Handler) shiftCancelScreen(w http.ResponseWriter, r *http.Request) {
	h.shiftMutation(w, r, "shift-cancel")
}
func (h *Handler) shiftAssignScreen(w http.ResponseWriter, r *http.Request) {
	h.shiftMutation(w, r, "assign")
}
func (h *Handler) shiftUnassignScreen(w http.ResponseWriter, r *http.Request) {
	h.shiftMutation(w, r, "unassign")
}
func (h *Handler) shiftRemoveScreen(w http.ResponseWriter, r *http.Request) {
	h.renderAssignmentRemoval(w, r, FormState{}, 200)
}
func (h *Handler) renderAssignmentRemoval(w http.ResponseWriter, r *http.Request, f FormState, status int) {
	m, err := h.store.ShiftDetail(r.Context(), identity(r), r.PathValue("shift"), filterRequest(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if !m.CanEdit {
		h.problem(w, r, ErrDenied)
		return
	}
	var person Person
	for _, p := range m.Shift.Roster {
		if p.ID == r.PathValue("worker") {
			person = p
			break
		}
	}
	if person.ID == "" {
		h.problem(w, r, ErrNotFound)
		return
	}
	f.ID = "assignment-remove"
	h.respond(w, r, status, scheduleMeta("Einteilung entfernen"), m.Shell, AssignmentRemovalContent(m, person, f))
}
func (h *Handler) shiftMutation(w http.ResponseWriter, r *http.Request, action string) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	id := r.PathValue("shift")
	c := formCommand(r)
	c.Action = action
	c.ID = id
	if action == "unassign" {
		c.WorkerID = r.PathValue("worker")
		if !c.Confirm {
			f := formState(r, ErrInvalid)
			f.Summary = "Bitte die Entfernung ausdrücklich bestätigen."
			f.Errors["confirm"] = "Bitte die Entfernung bestätigen."
			h.renderAssignmentRemoval(w, r, f, 422)
			return
		}
	}
	_, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
			f := formState(r, err)
			if action == "unassign" {
				h.renderAssignmentRemoval(w, r, f, errorStatus(err))
				return
			}
			if action == "shift-cancel" {
				f.ID = "cancel"
			}
			h.renderShift(w, r, id, f, errorStatus(err))
			return
		}
		h.problem(w, r, err)
		return
	}
	destination := h.returnTo(r, r.PostForm.Get("return_to"), canonical("/shifts/"+id, filterValues(filterRequest(r))))
	notice := "saved"
	if action == "shift-cancel" {
		notice = "cancelled"
	}
	h.success(w, r, withNotice(destination, notice))
}
