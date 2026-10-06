package ui

import (
	"net/http"

	"github.com/a-h/templ"
)

func Register(mux *http.ServeMux) {
	mux.Handle("GET /design-system", templ.Handler(DesignSystem()))
}
