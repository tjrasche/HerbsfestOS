package scheduling_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tjrasche/HerbsfestOS/internal/auth"
	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
)

func uiHTTP(t *testing.T, app *s.Store, dbBootstrap func()) http.Handler {
	t.Helper()
	dbBootstrap()
	guard, err := auth.New(ctx, auth.Config{Mode: "development"})
	must(t, err)
	private, public := http.NewServeMux(), http.NewServeMux()
	s.NewHandler(app, "http://127.0.0.1:58081").Register(private, public, guard.Optional)
	public.Handle("/", guard.Protect(private, nil))
	return http.NewCrossOriginProtection().Handler(public)
}
func TestPrivateFullFrameHistoryAndFailClosedResponses(t *testing.T) {
	app, db := setup(t)
	_, _, shift := baseline(t, app)
	web := uiHTTP(t, app, func() { must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer")) })
	for _, tc := range []struct {
		hx, restore bool
		full        bool
	}{{false, false, true}, {true, false, false}, {true, true, true}} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.hx {
			r.Header.Set("HX-Request", "true")
		}
		if tc.restore {
			r.Header.Set("HX-History-Restore-Request", "true")
		}
		w := httptest.NewRecorder()
		web.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || strings.Count(body, `id="app-frame"`) != 1 {
			t.Fatalf("frame response invalid: %d %s", w.Code, body)
		}
		if got := strings.Contains(strings.ToLower(body), "<!doctype html>"); got != tc.full {
			t.Fatalf("full document=%t, want %t", got, tc.full)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Header().Get("Vary"), "HX-History-Restore-Request") {
			t.Fatal("private cache/header contract missing", w.Header())
		}
		if strings.Contains(body, "/actions") || strings.Contains(body, "name=\"worker_id\"") {
			t.Fatal("summary hydrated mutation forms")
		}
	}
	// The synthetic browser identity is global, but has no participation team.
	r := httptest.NewRequest("GET", "/shifts/"+shift+"/edit", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	web.ServeHTTP(w, r)
	if w.Code != 403 || strings.Contains(w.Body.String(), "Aufbau") || strings.Contains(w.Body.String(), "Helfer suchen") {
		t.Fatal("global without team received edit state", w.Code, w.Body.String())
	}
	app.FGA = nil
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	web.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "Aufbau") || strings.Count(w.Body.String(), `id="app-frame"`) != 1 {
		t.Fatal("outage left protected content", w.Code, w.Body.String())
	}
}

func TestEditionValidationPreservesInputsAndResourceContext(t *testing.T) {
	app, db := setup(t)
	web := uiHTTP(t, app, func() { must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer")) })
	values := url.Values{"label": {"Eintrag bleibt erhalten"}, "start_day": {"2027-09-12"}, "end_day": {"2027-09-10"}}
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/admin/editions", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		web.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 422 || !strings.Contains(body, "Eintrag bleibt erhalten") || !strings.Contains(body, "2027-09-12") || !strings.Contains(body, "2027-09-10") || !strings.Contains(body, "form-errors") {
			t.Fatal("invalid edition input lost", w.Code, body)
		}
		if !strings.Contains(body, `aria-invalid="true"`) {
			t.Fatal("field-specific error missing", body)
		}
	}
}

func TestEditionSelectionMapsPersistentStandAndKeepsPageContext(t *testing.T) {
	app, db := setup(t)
	edition, part, _ := baseline(t, app)
	next := exec(t, app, s.Command{Action: "edition-save", Label: "Nächste Ausgabe", StartDay: "2028-09-08", EndDay: "2028-09-10", SourceID: edition}).ID
	var copiedPart string
	must(t, db.Raw("SELECT p.id FROM participations p JOIN participations old ON old.stand_id=p.stand_id WHERE old.id=? AND p.edition_id=?", part, next).Scan(&copiedPart).Error)
	web := uiHTTP(t, app, func() { must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer")) })
	for _, path := range []string{"/", "/directory", "/feedback-notes"} {
		from := path + "?edition=" + edition + "&stand=" + part + "&day=2027-09-10&q=Beispiel"
		values := url.Values{"edition": {next}, "return_to": {from}}
		r := httptest.NewRequest("POST", "/edition-selection", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		web.ServeHTTP(w, r)
		if w.Code != 303 {
			t.Fatal("selection failed", w.Code, w.Body.String())
		}
		location, err := url.Parse(w.Header().Get("Location"))
		must(t, err)
		wantStand := copiedPart
		if path == "/directory" {
			wantStand = ""
		}
		if location.Path != path || location.Query().Get("edition") != next || location.Query().Get("stand") != wantStand || location.Query().Get("day") != "" || location.Query().Get("q") != "Beispiel" {
			t.Fatalf("selection lost scope: %s", location)
		}
	}
	// A query string cannot override the edition that owns this record route.
	r := httptest.NewRequest("GET", "/admin/editions/"+next+"?edition="+edition, nil)
	w := httptest.NewRecorder()
	web.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Nächste Ausgabe") {
		t.Fatal("route-owned edition lost", w.Code)
	}
}
