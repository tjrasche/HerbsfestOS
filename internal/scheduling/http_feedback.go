package scheduling

import "net/http"

func (h *Handler) renderOrgFeedback(w http.ResponseWriter, r *http.Request, status int, f FormState) {
	f.ID = "feedback"
	v, err := h.store.FeedbackList(r.Context(), identity(r), requestEdition(r), pageNumber(r))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.respond(w, r, status, orgMeta("Feedback", "feedback"), v.Shell, FeedbackContent(v, f))
}
func (h *Handler) orgFeedback(w http.ResponseWriter, r *http.Request) {
	h.renderOrgFeedback(w, r, 200, orgForm(r, nil))
}
func (h *Handler) orgFeedbackSave(w http.ResponseWriter, r *http.Request) {
	if err := parse(w, r); err != nil {
		h.problem(w, r, err)
		return
	}
	title := r.PostForm.Get("title")
	err := h.store.AddFeedback(r.Context(), identity(r), requestEdition(r), title)
	if err != nil {
		f := orgForm(r, err)
		f.Errors["title"] = "Bitte eine Notiz mit 1 bis 200 Zeichen eingeben."
		h.renderOrgFeedback(w, r, errorStatus(err), f)
		return
	}
	h.success(w, r, orgDestination(canonical("/feedback-notes", r.URL.Query()), "saved"))
}
