package scheduling

import "net/http"

func (h *Handler) printScreen(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.PrintSchedule(r.Context(), identity(r), r.PathValue("id"), requestEdition(r), r.URL.Query().Get("day"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	responseHeaders(w)
	if err = PrintRoster(m).Render(r.Context(), w); err != nil {
		return
	}
}
