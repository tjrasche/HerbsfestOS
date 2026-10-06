package feedbacknote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type memoryRepository struct {
	notes []FeedbackNote
	err   error
}

func (r *memoryRepository) List(context.Context) ([]FeedbackNote, error) { return r.notes, r.err }
func (r *memoryRepository) Create(_ context.Context, note *FeedbackNote) error {
	if r.err != nil {
		return r.err
	}
	r.notes = append(r.notes, *note)
	return nil
}

func TestCreateFeedbackNote(t *testing.T) {
	for _, tc := range []struct {
		name, title   string
		htmx          bool
		status, count int
	}{
		{"form redirects", " Improve signs ", false, 303, 1},
		{"htmx returns fragment", "<script>alert(1)</script>", true, 200, 1},
		{"invalid form renders page", " ", false, 422, 0},
		{"invalid htmx renders fragment", " ", true, 422, 0},
		{"long title rejected", strings.Repeat("x", 201), true, 422, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryRepository{}
			mux := http.NewServeMux()
			NewHandler(NewService(repo)).Register(mux)
			r := httptest.NewRequest("POST", "/feedback-notes", strings.NewReader(url.Values{"title": {tc.title}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.htmx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status || len(repo.notes) != tc.count {
				t.Fatalf("status=%d, notes=%d", w.Code, len(repo.notes))
			}
			if tc.status == 303 {
				if w.Header().Get("Location") != "/" || repo.notes[0].Title != "Improve signs" {
					t.Fatal("expected redirect and normalized title")
				}
				return
			}
			body := w.Body.String()
			if strings.Contains(body, "<!doctype html>") == tc.htmx {
				t.Fatal("incorrect page/fragment response")
			}
			if !strings.Contains(body, `id="notes"`) {
				t.Fatal("missing swap target")
			}
			if tc.title[0] == '<' && (!strings.Contains(body, "&lt;script&gt;") || strings.Contains(body, tc.title)) {
				t.Fatal("title was not escaped")
			}
		})
	}
}

func TestBrowserCrossOriginPostRejected(t *testing.T) {
	repo := &memoryRepository{}
	mux := http.NewServeMux()
	NewHandler(NewService(repo)).Register(mux)
	r := httptest.NewRequest("POST", "/feedback-notes", strings.NewReader("title=Unexpected"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	http.NewCrossOriginProtection().Handler(mux).ServeHTTP(w, r)
	if w.Code != 403 || len(repo.notes) != 0 {
		t.Fatal("cross-origin request must not create a note")
	}
}

func TestRepositoryFailureIsNotExposed(t *testing.T) {
	repo := &memoryRepository{err: errors.New("private database details")}
	mux := http.NewServeMux()
	NewHandler(NewService(repo)).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("expected generic server error")
	}
}
