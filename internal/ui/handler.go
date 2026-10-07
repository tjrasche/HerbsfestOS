package ui

import (
	"net/http"

	"github.com/a-h/templ"
)

func Register(mux *http.ServeMux) {
	mux.Handle("GET /design-system", templ.Handler(DesignSystem()))
}

// RegisterPublic receives a mux mounted at /public; paths are relative to it.
func RegisterPublic(mux *http.ServeMux) {
	mux.Handle("GET /{$}", templ.Handler(PublicPage()))
}

func AccessDenied() http.Handler {
	return templ.Handler(AccessDeniedPage(), templ.WithStatus(http.StatusForbidden))
}
