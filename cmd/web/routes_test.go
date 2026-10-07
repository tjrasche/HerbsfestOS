package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicRouteGroupBypassesAuth(t *testing.T) {
	for _, tc := range []struct {
		method, path, location string
		status                 int
		protected              bool
	}{
		{http.MethodGet, "/public", "/public/", http.StatusTemporaryRedirect, false},
		{http.MethodGet, "/public?festival=2026", "/public/?festival=2026", http.StatusTemporaryRedirect, false},
		{http.MethodGet, "/public/", "", http.StatusOK, false},
		{http.MethodHead, "/public/", "", http.StatusOK, false},
		{http.MethodPost, "/public/", "", http.StatusMethodNotAllowed, false},
		{http.MethodGet, "/public/missing", "", http.StatusNotFound, false},
		{http.MethodGet, "/public/design-system", "", http.StatusNotFound, false},
		{http.MethodPost, "/public/feedback-notes", "", http.StatusNotFound, false},
		{http.MethodGet, "/", "/auth/login", http.StatusSeeOther, true},
		{http.MethodGet, "/design-system", "/auth/login", http.StatusSeeOther, true},
		{http.MethodPost, "/feedback-notes", "/auth/login", http.StatusSeeOther, true},
		{http.MethodGet, "/publicity", "/auth/login", http.StatusSeeOther, true},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			guardCalled := false
			guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				guardCalled = true
				http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			})
			app := http.NewCrossOriginProtection().Handler(newMux(guard))
			w := httptest.NewRecorder()
			// No cookie or Authorization header: public access must be anonymous.
			app.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || w.Header().Get("Location") != tc.location || guardCalled != tc.protected {
				t.Fatalf("status=%d, location=%q, auth called=%t", w.Code, w.Header().Get("Location"), guardCalled)
			}
			if !tc.protected && (w.Header().Get("HX-Redirect") != "" || len(w.Result().Cookies()) != 0) {
				t.Fatal("public request must not start an auth session or htmx login redirect")
			}
		})
	}
}
