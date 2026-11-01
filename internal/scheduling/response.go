package scheduling

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func responseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Vary", "HX-Request, HX-History-Restore-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

// respond is the only full/frame response boundary. Rendering is buffered so a
// component error can never leave an incomplete private frame in the browser.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, status int, meta ui.PageMeta, shell ui.ShellModel, body templ.Component) {
	responseHeaders(w)
	if shell.Private {
		shell.ReturnURL = safeReturn(r.URL.RequestURI(), "/")
	}
	component := ui.Page(meta, shell, body)
	if isHTMX(r) {
		component = ui.Frame(meta, shell, body)
	}
	var b bytes.Buffer
	if err := component.Render(r.Context(), &b); err != nil {
		http.Error(w, "Die Seite konnte nicht angezeigt werden.", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func errorStatus(err error) int {
	switch {
	case errors.Is(err, ErrDenied):
		return 403
	case errors.Is(err, ErrUnavailable):
		return 503
	case errors.Is(err, ErrInvalid):
		return 422
	case errors.Is(err, ErrConflict):
		return 409
	case errors.Is(err, ErrNotFound):
		return 404
	}
	return 500
}
func errorMessage(err error) string {
	if errorStatus(err) != 500 {
		return err.Error()
	}
	return "Die Aktion konnte derzeit nicht abgeschlossen werden. Bitte erneut versuchen."
}
func (h *Handler) problem(w http.ResponseWriter, r *http.Request, err error) {
	status := errorStatus(err)
	if status == 503 {
		w.Header().Set("Retry-After", "5")
	}
	// Deliberately do not reuse a role or page snapshot after denied/unavailable.
	meta := ui.PageMeta{Title: "Seite nicht verfügbar · HerbstfestOS", Active: "error", Heading: "Seite nicht verfügbar"}
	h.respond(w, r, status, meta, ui.ShellModel{}, ui.ErrorContent(status, errorMessage(err)))
}
func (h *Handler) success(w http.ResponseWriter, r *http.Request, destination string) {
	if isHTMX(r) {
		w.Header().Set("HX-Replace-Url", destination)
		get := r.Clone(r.Context())
		get.Method = http.MethodGet
		get.Body = nil
		get.ContentLength = 0
		u, err := r.URL.Parse(destination)
		if err != nil {
			h.problem(w, r, ErrInvalid)
			return
		}
		get.URL = u
		get.Form = nil
		get.PostForm = nil
		h.private.ServeHTTP(w, get)
		return
	}
	responseHeaders(w)
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
func boundedPage(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	if n > 10000 {
		return 10000
	}
	return n
}
