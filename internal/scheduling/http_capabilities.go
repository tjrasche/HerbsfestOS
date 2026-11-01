package scheduling

import (
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/http"
)

func (h *Handler) registerCapabilities(m *http.ServeMux) {
	m.HandleFunc("GET /admin/invitations", h.orgInvitations)
	m.HandleFunc("POST /admin/invitations", h.orgIssueInvitation)
	m.HandleFunc("GET /links/{capability}", h.orgLink)
	m.HandleFunc("POST /links/{capability}/reissue", h.orgReissueLink)
	m.HandleFunc("POST /links/{capability}/revoke", h.orgRevokeLink)
	m.HandleFunc("POST /participations/{participation}/links", h.orgIssueSignupLink)
	m.HandleFunc("POST /shifts/{shift}/links", h.orgIssueSignupLink)
	m.HandleFunc("POST /workers/{worker}/connection-links", h.orgIssueConnectionLink)
}
func (h *Handler) orgInvitations(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.Capabilities(r.Context(), identity(r), requestEdition(r), r.URL.Query().Get("scope"), r.URL.Query().Get("status"), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, 200, orgMeta("Einladungen", "admin"), v.Shell, CapabilityListContent(v, orgForm(r, nil)))
}
func (h *Handler) renderOrgLink(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	v, err := h.store.CapabilityDetail(r.Context(), identity(r), r.PathValue("capability"), requestEdition(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, linkMeta(v.Shell, "Link verwalten"), v.Shell, CapabilityDetailContent(v, f))
}
func (h *Handler) orgLink(w http.ResponseWriter, r *http.Request) {
	h.renderOrgLink(w, r, 200, orgForm(r, nil))
}
func (h *Handler) issuePresentedLink(w http.ResponseWriter, r *http.Request, c Command, back string) {
	// Validate trusted origin before committing a one-time secret that could not
	// otherwise be presented safely. Never derive it from request headers.
	if _, err := h.absoluteLink("/proof/preview"); err != nil {
		h.problem(w, r, err)
		return
	}
	out, err := h.store.Execute(r.Context(), identity(r), c)
	if err != nil {
		h.linkIssueError(w, r, c, err)
		return
	}
	path := "/proof/" + out.Token
	if c.Action == "signup-link" {
		path = "/register/" + out.Token
	}
	if c.Action == "capability-reissue" {
		v, e := h.store.CapabilityDetail(r.Context(), identity(r), out.ID, requestEdition(r))
		if e != nil {
			h.problem(w, r, e)
			return
		}
		if v.Link.Kind == "signup" {
			path = "/register/" + out.Token
		}
	}
	absolute, err := h.absoluteLink(path)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	shell, err := h.store.Shell(r.Context(), identity(r), requestEdition(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	back = h.returnTo(r, r.PostForm.Get("return_to"), orgPageURL(back, shell))
	h.respond(w, r, 200, linkMeta(shell, "Link erstellt"), shell, CreatedLinkContent(absolute, back, orgPageURL("/links/"+out.ID, shell)))
}
func (h *Handler) linkIssueError(w http.ResponseWriter, r *http.Request, c Command, err error) {
	if c.CapabilityID != "" {
		h.renderOrgLink(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	// Read the shell again so a denied/unavailable write never keeps stale grants.
	shell, e := h.store.Shell(r.Context(), identity(r), requestEdition(r))
	if e != nil {
		h.problem(w, r, e)
		return
	}
	h.respond(w, r, errorStatus(err), linkMeta(shell, "Link konnte nicht erstellt werden"), shell, LinkFailureContent(orgForm(r, err)))
}
func linkMeta(shell ui.ShellModel, title string) ui.PageMeta {
	if shell.Global {
		return orgMeta(title, "admin")
	}
	return ui.PageMeta{Title: title + " · HerbstfestOS", Active: "schedule", Heading: title, Breadcrumbs: []ui.Breadcrumb{{Label: "Dienstplanung", Href: orgPageURL("/", shell)}, {Label: title}}}
}
func (h *Handler) orgIssueInvitation(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	c := Command{Action: "organizer-invite", Scope: r.PostForm.Get("scope"), ParticipationID: r.PostForm.Get("participation_id")}
	back := "/admin/invitations"
	if c.Scope == "team" {
		var p Participation
		err := h.store.transact(r.Context(), identity(r), false, func(t *Tx) error {
			if err := t.admin("issue_organizer_invite"); err != nil {
				return err
			}
			var err error
			p, err = t.participation(c.ParticipationID)
			return err
		})
		if err != nil {
			h.problem(w, r, err)
			return
		}
		back = "/admin/editions/" + p.EditionID + "/participations/" + p.ID
	}
	h.issuePresentedLink(w, r, c, back)
}
func (h *Handler) orgIssueSignupLink(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	c := Command{Action: "signup-link", ParticipationID: r.PathValue("participation"), ID: r.PathValue("shift")}
	back := "/"
	var shell ui.ShellModel
	var scope string
	err := h.store.transact(r.Context(), identity(r), false, func(t *Tx) error {
		var p Participation
		var err error
		if c.ID != "" {
			var sh Shift
			sh, p, err = t.shift(c.ID, false)
			scope = sh.Title + " · " + p.Name
		} else {
			p, err = t.participation(c.ParticipationID)
			scope = p.Name
		}
		if err != nil {
			return err
		}
		c.ParticipationID = p.ID
		if t.global {
			err = t.admin("manage_catalog")
		} else {
			err = t.permitPart(p, "edit_schedule")
		}
		if err != nil {
			return err
		}
		shell, err = t.shell(p.EditionID)
		if err != nil {
			return err
		}
		if c.ID != "" {
			back = orgPageURL("/shifts/"+c.ID, shell)
		} else if shell.Global {
			back = participationPath(p.EditionID, p.ID)
		} else {
			back = canonical("/", contextQuery(shell))
			q := contextQuery(shell)
			q.Set("stand", p.ID)
			back = canonical("/", q)
		}
		return nil
	})
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if r.PostForm.Get("confirm") != "true" {
		h.confirmLinkReplacement(w, r, shell, scope, back)
		return
	}
	h.issuePresentedLink(w, r, c, back)
}
func (h *Handler) orgIssueConnectionLink(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	id := r.PathValue("worker")
	var shell ui.ShellModel
	var worker Worker
	err := h.store.transact(r.Context(), identity(r), false, func(t *Tx) error {
		if err := t.connectionPermission(id); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT * FROM workers WHERE id=?", id).Scan(&worker).Error; err != nil {
			return err
		}
		if worker.ID == "" {
			return ErrDenied
		}
		var err error
		shell, err = t.shell(requestEdition(r))
		return err
	})
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if r.PostForm.Get("confirm") != "true" {
		h.confirmLinkReplacement(w, r, shell, worker.Name(), orgPageURL("/workers/"+id, shell))
		return
	}
	h.issuePresentedLink(w, r, Command{Action: "connection-link", WorkerID: id}, "/workers/"+id)
}
func (h *Handler) confirmLinkReplacement(w http.ResponseWriter, r *http.Request, shell ui.ShellModel, scope, back string) {
	f := formState(r, ErrInvalid)
	f.ID = "link-replacement"
	f.Summary = "Es wurde kein Link erstellt oder ersetzt. Bitte die Ersetzung ausdrücklich bestätigen."
	f.Errors["confirm"] = "Bitte bestätigen, dass ein bisheriger Link ungültig wird."
	back = h.returnTo(r, r.PostForm.Get("return_to"), back)
	action := orgPageURL(r.URL.Path, shell)
	h.respond(w, r, http.StatusUnprocessableEntity, linkMeta(shell, "Linkersetzung bestätigen"), shell, LinkReplacementContent(action, back, scope, f))
}
func (h *Handler) orgReissueLink(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	if r.PostForm.Get("confirm") != "true" {
		h.renderOrgLink(w, r, 422, orgForm(r, ErrInvalid))
		return
	}
	id := r.PathValue("capability")
	h.issuePresentedLink(w, r, Command{Action: "capability-reissue", CapabilityID: id}, "/links/"+id)
}
func (h *Handler) orgRevokeLink(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	var err error
	if r.PostForm.Get("confirm") != "true" {
		err = ErrInvalid
	} else {
		_, err = h.store.Execute(r.Context(), identity(r), Command{Action: "capability-revoke", CapabilityID: r.PathValue("capability")})
	}
	if err != nil {
		h.renderOrgLink(w, r, errorStatus(err), orgForm(r, err))
		return
	}
	h.success(w, r, orgResultDestination(r, "/links/"+r.PathValue("capability"), "updated"))
}
