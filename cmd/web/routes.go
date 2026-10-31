package main

import (
	"net/http"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func newMux(protected http.Handler) *http.ServeMux {
	public := http.NewServeMux()
	ui.RegisterPublic(public)

	mux := http.NewServeMux()
	// Public handlers register paths relative to /public, independently of auth.
	mux.Handle("/public/", http.StripPrefix("/public", public))
	mux.Handle("GET /static/", ui.Assets())
	ui.Register(mux)
	mux.Handle("/", protected)
	return mux
}
