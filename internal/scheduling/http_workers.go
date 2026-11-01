package scheduling

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func (h *Handler) registerPeople(m *http.ServeMux) {
	m.HandleFunc("GET /directory", h.peopleDirectory)
	m.HandleFunc("GET /workers/{worker}", h.peopleWorker)
	m.HandleFunc("POST /workers/{worker}", h.peopleSaveWorker)
	m.HandleFunc("GET /workers/{worker}/assign", h.peopleAssignment)
	m.HandleFunc("POST /workers/{worker}/assign", h.peopleAssign)
	m.HandleFunc("POST /workers/{worker}/memberships", h.peopleExistingMember)
	m.HandleFunc("GET /stands/{stand}/members", h.peopleMembers)
	m.HandleFunc("POST /stands/{stand}/members", h.peopleAddMember)
	m.HandleFunc("POST /stands/{stand}/members/{worker}/remove", h.peopleRemoveMember)
	m.HandleFunc("GET /admin/workers/merge", h.peopleMerge)
	m.HandleFunc("POST /admin/workers/merge", h.peopleMergeSave)
}
func peopleMeta(title string) ui.PageMeta {
	return ui.PageMeta{Title: title + " · HerbstfestOS", Active: "directory", Heading: title, Breadcrumbs: []ui.Breadcrumb{{Label: "Helfer suchen", Href: "/directory"}, {Label: title}}}
}
func (h *Handler) peopleDirectory(w http.ResponseWriter, r *http.Request) {
	q := DirectoryQuery{Edition: requestEdition(r), Query: r.URL.Query().Get("q"), Stand: r.URL.Query().Get("stand"), Day: r.URL.Query().Get("day"), Shift: r.URL.Query().Get("shift"), MemberStand: r.URL.Query().Get("member_stand"), ReturnTo: h.returnTo(r, r.URL.Query().Get("return_to"), "/"), Page: pageNumber(r)}
	v, err := h.store.SearchWorkers(r.Context(), identity(r), q)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, peopleMeta("Helfer suchen"), v.Shell, DirectoryContent(v, orgForm(r, nil)))
}
func (h *Handler) renderPeopleWorker(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "worker"
	v, err := h.store.WorkerDetail(r.Context(), identity(r), requestEdition(r), r.PathValue("worker"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	meta := peopleMeta(v.Worker.Name())
	if v.Worker.ID == v.Shell.WorkerID {
		meta.Active = "worker"
		meta.Breadcrumbs = []ui.Breadcrumb{{Label: "Meine Anfragen", Href: "/requests/mine"}, {Label: "Meine Angaben"}}
	}
	h.respond(w, r, status, meta, v.Shell, WorkerProfileContent(v, f))
}
func (h *Handler) peopleWorker(w http.ResponseWriter, r *http.Request) {
	h.renderPeopleWorker(w, r, 200, orgForm(r, nil))
}
func (h *Handler) peopleSaveWorker(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	_, notesPresent := r.PostForm["notes"]
	c := Command{Action: "worker-save", ID: r.PathValue("worker"), FirstName: r.PostForm.Get("first_name"), LastName: r.PostForm.Get("last_name"), Email: r.PostForm.Get("email"), Phone: r.PostForm.Get("phone"), Notes: r.PostForm.Get("notes"), PreserveNotes: !notesPresent}
	_, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		f := orgForm(r, err)
		if !textValid(c.FirstName, 120) {
			f.Errors["first_name"] = "Bitte den Vornamen eingeben (höchstens 120 Zeichen)."
		}
		if !textValid(c.LastName, 120) {
			f.Errors["last_name"] = "Bitte den Nachnamen eingeben (höchstens 120 Zeichen)."
		}
		if len(c.Email) > 320 {
			f.Errors["email"] = "Die Kontakt-E-Mail ist zu lang."
		}
		if len(c.Phone) > 80 {
			f.Errors["phone"] = "Die Telefonnummer ist zu lang."
		}
		if len(c.Notes) > 20000 {
			f.Errors["notes"] = "Die Notiz ist zu lang. Bitte kürzen."
		}
		h.renderPeopleWorker(w, r, errorStatus(err), f)
		return
	}
	dest := canonical("/workers/"+c.ID, r.URL.Query())
	h.success(w, r, orgDestination(dest, "saved"))
}
func (h *Handler) renderPeopleAssignment(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	v, err := h.store.AssignmentChoices(r.Context(), identity(r), r.PathValue("worker"), ScheduleFilter{Edition: requestEdition(r), Stand: r.URL.Query().Get("stand"), Day: r.URL.Query().Get("day"), Page: pageNumber(r)}, r.URL.Query().Get("q"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	v.Shift = r.URL.Query().Get("shift")
	v.ReturnTo = h.returnTo(r, r.URL.Query().Get("return_to"), "")
	if v.Shift != "" {
		ok := false
		for _, choice := range v.Choices {
			if choice.ID == v.Shift {
				ok = true
				break
			}
		}
		if !ok {
			v.Shift = ""
		}
	}
	h.respond(w, r, status, peopleMeta("Helfer einteilen"), v.Shell, AssignmentContent(v, f))
}
func (h *Handler) peopleAssignment(w http.ResponseWriter, r *http.Request) {
	h.renderPeopleAssignment(w, r, 200, orgForm(r, nil))
}
func (h *Handler) peopleAssign(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	// The chosen worker is path-owned; the selected shift is resolved again by
	// Execute and assignment plus membership commit as one command.
	_, err := h.store.Execute(r.Context(), identity(r), Command{Action: "assign", ID: r.PostForm.Get("shift_id"), WorkerID: r.PathValue("worker")})
	if err != nil {
		h.renderPeopleAssignment(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	dest := h.returnTo(r, r.PostForm.Get("return_to"), "/shifts/"+r.PostForm.Get("shift_id"))
	h.success(w, r, orgDestination(dest, "saved"))
}
func (h *Handler) peopleExistingMember(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	_, err := h.store.Execute(r.Context(), identity(r), Command{Action: "member-add", WorkerID: r.PathValue("worker"), StandID: r.PostForm.Get("stand_id")})
	if err != nil {
		h.renderPeopleAssignment(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	h.success(w, r, orgDestination(canonical("/workers/"+r.PathValue("worker")+"/assign", r.URL.Query()), "saved"))
}
func (h *Handler) renderPeopleMembers(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "member"
	v, err := h.store.Memberships(r.Context(), identity(r), requestEdition(r), r.PathValue("stand"), r.URL.Query().Get("q"), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, peopleMeta(v.Stand.Name+" · Helfer"), v.Shell, MembershipContent(v, f))
}
func (h *Handler) peopleMembers(w http.ResponseWriter, r *http.Request) {
	h.renderPeopleMembers(w, r, 200, orgForm(r, nil))
}
func (h *Handler) peopleAddMember(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	c := Command{Action: "member-add", StandID: r.PathValue("stand"), WorkerID: r.PostForm.Get("worker_id"), FirstName: r.PostForm.Get("first_name"), LastName: r.PostForm.Get("last_name")}
	_, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		f := orgForm(r, err)
		if c.WorkerID == "" {
			if !textValid(c.FirstName, 120) {
				f.Errors["first_name"] = "Bitte Vornamen eingeben."
			}
			if !textValid(c.LastName, 120) {
				f.Errors["last_name"] = "Bitte Nachnamen eingeben."
			}
		}
		h.renderPeopleMembers(w, r, errorStatus(err), f)
		return
	}
	destination := h.returnTo(r, r.PostForm.Get("return_to"), canonical("/stands/"+c.StandID+"/members", r.URL.Query()))
	h.success(w, r, orgDestination(destination, "saved"))
}
func (h *Handler) peopleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	var err error
	if r.PostForm.Get("confirm") != "true" {
		err = ErrInvalid
	} else {
		_, err = h.store.Execute(r.Context(), identity(r), Command{Action: "member-remove", StandID: r.PathValue("stand"), WorkerID: r.PathValue("worker")})
	}
	if err != nil {
		h.renderPeopleMembers(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	h.success(w, r, orgDestination(canonical("/stands/"+r.PathValue("stand")+"/members", r.URL.Query()), "removed"))
}
func (h *Handler) renderPeopleMerge(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "merge"
	target, source := r.URL.Query().Get("target"), r.URL.Query().Get("source")
	if r.Method == http.MethodPost {
		target = r.PostForm.Get("target")
		source = r.PostForm.Get("source")
	}
	v, err := h.store.MergeCandidates(r.Context(), identity(r), requestEdition(r), target, source, r.URL.Query().Get("q"), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, orgMeta("Helfer zusammenführen", "admin"), v.Shell, MergeContent(v, f))
}
func (h *Handler) peopleMerge(w http.ResponseWriter, r *http.Request) {
	h.renderPeopleMerge(w, r, 200, orgForm(r, nil))
}
func (h *Handler) peopleMergeSave(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	f := r.PostForm
	c := Command{Action: "worker-merge", ID: f.Get("target"), OtherWorkerID: f.Get("source"), FirstName: f.Get("first_name"), LastName: f.Get("last_name"), Email: f.Get("email"), Phone: f.Get("phone"), Notes: f.Get("notes"), Confirm: f.Get("confirm") == "true", PreviewFingerprint: f.Get("fingerprint"), EmailChoice: f.Get("email_choice"), PhoneChoice: f.Get("phone_choice"), ClearEmail: f.Get("clear_email") == "true", ClearPhone: f.Get("clear_phone") == "true", ClearNotes: f.Get("clear_notes") == "true"}
	_, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		state := orgForm(r, err)
		if errors.Is(err, ErrConflict) {
			state.Summary = "Die Datensätze wurden geändert oder besitzen unterschiedliche Konten. Bitte den aktualisierten Vergleich prüfen und erneut bestätigen. Deine vorgeschlagenen Angaben bleiben erhalten."
		}
		h.renderPeopleMerge(w, r, errorStatus(err), state)
		return
	}
	h.success(w, r, orgResultDestination(r, "/workers/"+c.ID, "saved"))
}
func directoryContext(v DirectoryPageModel) url.Values {
	q := contextQuery(v.Shell)
	if v.MemberStand.ID != "" {
		q.Set("member_stand", v.MemberStand.ID)
	}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	if v.Stand != "" {
		q.Set("stand", v.Stand)
	}
	if v.Day != "" {
		q.Set("day", v.Day)
	}
	if v.Shift != "" {
		q.Set("shift", v.Shift)
	}
	if v.ReturnTo != "" {
		q.Set("return_to", v.ReturnTo)
	}
	return q
}
func directoryPageQuery(v DirectoryPageModel) url.Values {
	q := directoryContext(v)
	if v.Page > 1 {
		q.Set("page", n(v.Page))
	}
	return q
}
func directoryPageURL(v DirectoryPageModel) string {
	return canonical("/directory", directoryPageQuery(v))
}
func membershipDirectoryURL(v MembershipPage) string {
	q := contextQuery(v.Shell)
	q.Set("member_stand", v.Stand.ID)
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	return canonical("/directory", q)
}
func assignmentContext(v AssignmentPage) url.Values {
	q := contextQuery(v.Shell)
	if v.Stand != "" {
		q.Set("stand", v.Stand)
	}
	if v.Day != "" {
		q.Set("day", v.Day)
	}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	if v.Shift != "" {
		q.Set("shift", v.Shift)
	}
	if v.ReturnTo != "" {
		q.Set("return_to", v.ReturnTo)
	}
	return q
}
