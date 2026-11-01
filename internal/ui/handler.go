package ui

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
)

func Register(mux *http.ServeMux) {
	mux.Handle("GET /design-system", referenceHandler(DesignSystem(), http.StatusOK))
}

// RegisterPublic receives a mux mounted at /public; paths are relative to it.
func RegisterPublic(mux *http.ServeMux) {
	mux.Handle("GET /{$}", referenceHandler(PublicPage(), http.StatusOK))
}

func AccessDenied() http.Handler {
	return referenceHandler(AccessDeniedPage(), http.StatusForbidden)
}

type fragmentKey struct{}

func referenceFragment(ctx context.Context) bool {
	partial, _ := ctx.Value(fragmentKey{}).(bool)
	return partial
}
func referenceHandler(component templ.Component, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Vary", "HX-Request, HX-History-Restore-Request")
		partial := r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
		r = r.WithContext(context.WithValue(r.Context(), fragmentKey{}, partial))
		templ.Handler(component, templ.WithStatus(status)).ServeHTTP(w, r)
	})
}
