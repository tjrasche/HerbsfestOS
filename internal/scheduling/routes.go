package scheduling

import "net/http"

func (h *Handler) Register(private, public *http.ServeMux, optional func(http.Handler) http.Handler) {
	h.private = private
	h.registerSchedule(private, public, optional)
	h.registerOrganisation(private)
	private.HandleFunc("POST /edition-selection", h.selectEdition)
	// Compatibility adapter only; all new forms use resource-specific routes.
	private.HandleFunc("POST /actions", h.action)
	private.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { h.problem(w, r, ErrNotFound) })
}
