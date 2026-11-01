package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReferencePagesKeepChildrenAndFrameContract(t *testing.T) {
	for _, tc := range []struct {
		path, text string
		public     bool
	}{{"/design-system", "Designsystem", false}, {"/", "Ein Fest, das verbindet.", true}} {
		mux := http.NewServeMux()
		if tc.public {
			RegisterPublic(mux)
		} else {
			Register(mux)
		}
		for _, partial := range []bool{false, true} {
			r := httptest.NewRequest("GET", tc.path, nil)
			if partial {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != 200 || !strings.Contains(body, tc.text) || !strings.Contains(body, "<h1") || strings.Count(body, `id="app-frame"`) != 1 {
				t.Fatalf("%s partial=%v lost page content: status%d", tc.path, partial, w.Code)
			}
			if full := strings.Contains(strings.ToLower(body), "<!doctype html>"); full == partial {
				t.Fatalf("%s partial=%v wrong document shape", tc.path, partial)
			}
			if partial && strings.Contains(body, "<script") {
				t.Fatal("scripts duplicated in frame")
			}
		}
	}
}
