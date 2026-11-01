package scheduling

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func (h *Handler) registerOrganisation(m *http.ServeMux) {
	m.HandleFunc("GET /admin", h.orgOverview)
	m.HandleFunc("GET /admin/editions", h.orgEditions)
	m.HandleFunc("GET /admin/editions/new", h.orgEdition)
	m.HandleFunc("GET /admin/editions/{edition}", h.orgEdition)
	m.HandleFunc("POST /admin/editions", h.orgSaveEdition)
	m.HandleFunc("POST /admin/editions/{edition}", h.orgSaveEdition)
	m.HandleFunc("POST /admin/editions/{edition}/activate", h.orgActivate)
	m.HandleFunc("POST /admin/editions/{edition}/participations", h.orgParticipationMutation)
	m.HandleFunc("GET /admin/editions/{edition}/participations/{participation}", h.orgParticipation)
	m.HandleFunc("POST /admin/editions/{edition}/participations/{participation}", h.orgParticipationMutation)
	m.HandleFunc("POST /admin/editions/{edition}/participations/{participation}/team", h.orgTeamMutation)
	m.HandleFunc("POST /admin/editions/{edition}/participations/{participation}/team/{account}/remove", h.orgTeamMutation)
	m.HandleFunc("GET /admin/stands", h.orgStands)
	m.HandleFunc("GET /admin/stands/new", h.orgStand)
	m.HandleFunc("GET /admin/stands/{stand}", h.orgStand)
	m.HandleFunc("POST /admin/stands", h.orgSaveStand)
	m.HandleFunc("POST /admin/stands/{stand}", h.orgSaveStand)
	m.HandleFunc("GET /admin/access", h.orgAccess)
	m.HandleFunc("POST /admin/access/{account}/remove", h.orgRemoveAccess)
	h.registerPeople(m)
	h.registerCapabilities(m)
	m.HandleFunc("GET /feedback-notes", h.orgFeedback)
	m.HandleFunc("POST /feedback-notes", h.orgFeedbackSave)
}
func orgMeta(title, active string) ui.PageMeta {
	return ui.PageMeta{Title: title + " · HerbstfestOS", Heading: title, Active: active, Breadcrumbs: []ui.Breadcrumb{{Label: "Organisation", Href: "/admin"}, {Label: title}}}
}
func orgMessage(r *http.Request) string {
	switch r.URL.Query().Get("notice") {
	case "saved":
		return "Änderungen gespeichert."
	case "created":
		return "Eintrag erstellt."
	case "removed":
		return "Zugriff oder Zugehörigkeit entfernt."
	case "activated":
		return "Diese Ausgabe ist jetzt für alle aktuell."
	case "updated":
		return "Status aktualisiert."
	}
	return ""
}
func orgDestination(path, code string) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	q.Set("notice", code)
	u.RawQuery = q.Encode()
	return u.String()
}
func orgResultDestination(r *http.Request, path, code string) string {
	q := url.Values{}
	for _, key := range []string{"edition", "stand", "day", "page", "q", "scope", "status"} {
		value := r.URL.Query().Get(key)
		if len(value) <= 200 && value != "" {
			q.Set(key, value)
		}
	}
	if q.Get("day") != "" && !validDay(q.Get("day")) {
		q.Del("day")
	}
	if q.Get("page") != "" {
		q.Set("page", n(boundedPage(q.Get("page"))))
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "admin" && parts[1] == "editions" && parts[2] != "new" {
		q.Set("edition", parts[2])
	}
	return orgDestination(canonical(path, q), code)
}
func orgForm(r *http.Request, err error) FormState {
	f := formState(r, err)
	if err == nil {
		f.Message = orgMessage(r)
	}
	return f
}
func (h *Handler) orgOverview(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.OrganisationOverview(r.Context(), identity(r), requestEdition(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, orgMeta("Organisation", "admin"), v.Shell, OrganisationOverviewContent(v, orgForm(r, nil)))
}
func (h *Handler) orgEditions(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.EditionList(r.Context(), identity(r), requestEdition(r), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, orgMeta("Ausgaben", "admin"), v.Shell, EditionListContent(v, orgForm(r, nil)))
}
func (h *Handler) renderOrgEdition(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "edition"
	v, err := h.store.EditionAdmin(r.Context(), identity(r), requestEdition(r), r.PathValue("edition"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	title := "Neue Ausgabe"
	if v.Edition.ID != "" {
		title = v.Edition.Label
	}
	h.respond(w, r, status, orgMeta(title, "admin"), v.Shell, EditionDetailContent(v, f))
}
func (h *Handler) orgEdition(w http.ResponseWriter, r *http.Request) {
	h.renderOrgEdition(w, r, 200, orgForm(r, nil))
}
func (h *Handler) orgSaveEdition(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	c := Command{Action: "edition-save", ID: r.PathValue("edition"), Label: r.PostForm.Get("label"), StartDay: r.PostForm.Get("start_day"), EndDay: r.PostForm.Get("end_day"), SourceID: r.PostForm.Get("source_id")}
	out, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		f := orgForm(r, err)
		if !textValid(c.Label, 80) || len(c.Label) > 80 {
			f.Errors["label"] = "Bitte eine Bezeichnung eingeben."
			if len(c.Label) > 80 {
				f.Errors["label"] = "Die Bezeichnung ist zu lang. Bitte kürzen."
			}
		}
		start, startErr := time.Parse("2006-01-02", c.StartDay)
		end, endErr := time.Parse("2006-01-02", c.EndDay)
		if startErr != nil || endErr != nil || end.Before(start) {
			f.Errors["end_day"] = "Bitte gültige Festtage wählen; das Ende liegt am oder nach dem Beginn."
		}
		h.renderOrgEdition(w, r, errorStatus(err), f)
		return
	}
	h.success(w, r, orgResultDestination(r, "/admin/editions/"+out.ID, "saved"))
}
func (h *Handler) orgActivate(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	_, err := h.store.Execute(r.Context(), identity(r), Command{Action: "activate", ID: r.PathValue("edition"), Confirm: r.PostForm.Get("confirm") == "true"})
	if err != nil {
		h.renderOrgEdition(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	h.success(w, r, orgResultDestination(r, "/admin/editions/"+r.PathValue("edition"), "activated"))
}
func (h *Handler) orgStands(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.StandList(r.Context(), identity(r), requestEdition(r), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, orgMeta("Stände", "admin"), v.Shell, StandListContent(v, orgForm(r, nil)))
}
func (h *Handler) renderOrgStand(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "stand"
	v, err := h.store.StandAdmin(r.Context(), identity(r), requestEdition(r), r.PathValue("stand"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	title := nonempty(v.Stand.Name, "Neuer Stand")
	h.respond(w, r, status, orgMeta(title, "admin"), v.Shell, StandDetailContent(v, f))
}
func (h *Handler) orgStand(w http.ResponseWriter, r *http.Request) {
	h.renderOrgStand(w, r, 200, orgForm(r, nil))
}
func (h *Handler) orgSaveStand(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	out, err := h.store.Execute(r.Context(), identity(r), Command{Action: "stand-save", ID: r.PathValue("stand"), Name: r.PostForm.Get("name")})
	if err != nil {
		f := orgForm(r, err)
		f.Errors["name"] = "Bitte einen Standnamen mit höchstens 120 Zeichen eingeben."
		h.renderOrgStand(w, r, errorStatus(err), f)
		return
	}
	h.success(w, r, orgResultDestination(r, "/admin/stands/"+out.ID, "saved"))
}
func (h *Handler) renderOrgParticipation(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	v, err := h.store.ParticipationAdmin(r.Context(), identity(r), requestEdition(r), r.PathValue("edition"), r.PathValue("participation"), r.URL.Query().Get("q"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, orgMeta(v.Participation.Name+" · "+v.Edition.Label, "admin"), v.Shell, ParticipationDetailContent(v, f))
}
func (h *Handler) orgParticipation(w http.ResponseWriter, r *http.Request) {
	h.renderOrgParticipation(w, r, 200, orgForm(r, nil))
}
func (h *Handler) orgParticipationMutation(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	parent, id := r.PathValue("edition"), r.PathValue("participation")
	if id != "" {
		if _, err := h.store.ParticipationAdmin(r.Context(), identity(r), requestEdition(r), parent, id, ""); err != nil {
			h.problem(w, r, err)
			return
		}
	}
	c := Command{Action: "participation", ID: id, EditionID: parent, StandID: r.PostForm.Get("stand_id"), Enabled: r.PostForm.Get("enabled") == "true"}
	var err error
	if !c.Enabled && r.PostForm.Get("confirm") != "true" {
		err = ErrInvalid
	} else {
		_, err = h.store.Execute(r.Context(), identity(r), c)
	}
	if err != nil {
		if id != "" {
			h.renderOrgParticipation(w, r, errorStatus(err), orgForm(r, err))
		} else {
			h.renderOrgEdition(w, r, errorStatus(err), orgForm(r, err))
		}
		return
	}
	dest := "/admin/editions/" + parent
	if id != "" {
		dest = participationPath(parent, id)
	}
	h.success(w, r, orgResultDestination(r, dest, "updated"))
}
func (h *Handler) orgTeamMutation(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	parent, id := r.PathValue("edition"), r.PathValue("participation")
	if _, err := h.store.ParticipationAdmin(r.Context(), identity(r), requestEdition(r), parent, id, ""); err != nil {
		h.problem(w, r, err)
		return
	}
	c := Command{Action: "team-add", ParticipationID: id, AccountID: r.PostForm.Get("account_id")}
	if account := r.PathValue("account"); account != "" {
		c.Action = "team-remove"
		c.AccountID = account
		if r.PostForm.Get("confirm") != "true" {
			h.renderOrgParticipation(w, r, 422, orgForm(r, ErrInvalid))
			return
		}
	}
	_, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		h.renderOrgParticipation(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	h.success(w, r, orgResultDestination(r, "/admin/editions/"+parent+"/participations/"+id, "updated"))
}
func (h *Handler) orgAccess(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.AccessAdmin(r.Context(), identity(r), requestEdition(r), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, orgMeta("Teams & Zugriff", "admin"), v.Shell, AccessContent(v, orgForm(r, nil)))
}
func (h *Handler) orgRemoveAccess(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	var err error
	if r.PostForm.Get("confirm") != "true" {
		err = ErrInvalid
	} else {
		_, err = h.store.Execute(r.Context(), identity(r), Command{Action: "global-remove", AccountID: r.PathValue("account")})
	}
	if err != nil {
		v, e := h.store.AccessAdmin(r.Context(), identity(r), requestEdition(r), pageNumber(r))
		if e != nil {
			h.problem(w, r, e)
			return
		}
		h.respond(w, r, errorStatus(err), orgMeta("Teams & Zugriff", "admin"), v.Shell, AccessContent(v, orgForm(r, err)))
		return
	}
	h.success(w, r, orgResultDestination(r, "/admin/access", "removed"))
}
func pageLinks(path string, q url.Values, page int, more bool) (string, string) {
	prev, next := "", ""
	if page > 1 {
		p := cloneValues(q)
		p.Set("page", n(page-1))
		prev = canonical(path, p)
	}
	if more {
		p := cloneValues(q)
		p.Set("page", n(page+1))
		next = canonical(path, p)
	}
	return prev, next
}
func accountIn(team []Person, id string) bool {
	for _, a := range team {
		if a.ID == id {
			return true
		}
	}
	return false
}
func linkKind(kind string) string {
	switch kind {
	case "organizer":
		return "Private Organisationseinladung"
	case "signup":
		return "Anmeldelink"
	case "connection", "fresh":
		return "Privater Kontolink"
	}
	return strings.TrimSpace(kind)
}
