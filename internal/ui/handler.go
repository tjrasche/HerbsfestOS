package ui

import (
	"net/http"

	"github.com/a-h/templ"
)

func Register(mux *http.ServeMux) {
	mux.Handle("GET /design-system", templ.Handler(DesignSystem()))
}

func AccessDenied() http.Handler {
	return templ.Handler(AccessDeniedPage(), templ.WithStatus(http.StatusForbidden))
}
