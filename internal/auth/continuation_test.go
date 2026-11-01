package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestContinuationRejectsBrowserAuthorityAndLoginLoops(t *testing.T) {
	for _, path := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example/", "/%5cevil.example/", "/%2f%2fevil.example/", "/auth/login?next=/", "/safe\r\nLocation:evil", "/safe#secret"} {
		if got := safeContinuation(path); got != "/" {
			t.Errorf("unsafe continuation %q accepted as %q", path, got)
		}
	}
	if got := safeContinuation("/proof/private-proof"); got != "/proof/private-proof" {
		t.Fatal("proof continuation lost", got)
	}
}

func TestExpiredPrivateHTMXNavigationPreservesOnlyGETDestination(t *testing.T) {
	h := &Handler{}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRequest(method, "https://app.example/directory?q=Anna", nil)
		r.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("private handler reached") }), nil).ServeHTTP(w, r)
		want := "/auth/login"
		if method == http.MethodGet {
			want += "?next=" + url.QueryEscape("/directory?q=Anna")
		}
		if w.Code != http.StatusUnauthorized || w.Header().Get("HX-Redirect") != want {
			t.Fatalf("%s: %d %v", method, w.Code, w.Header())
		}
	}
}
