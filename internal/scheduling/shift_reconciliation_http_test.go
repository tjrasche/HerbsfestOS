package scheduling_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"golang.org/x/net/html"
)

func TestShiftConflictReconciliationFullAndFrame(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "frame"}[hx], func(t *testing.T) {
			app, db := setup(t)
			_, part, shift := baseline(t, app)
			var account string
			web := uiHTTP(t, app, func() {
				must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer"))
				must(t, db.Raw("SELECT id FROM accounts WHERE issuer='urn:herbstfest:local' AND subject='developer'").Scan(&account).Error)
				exec(t, app, s.Command{Action: "team-add", ParticipationID: part, AccountID: account})
			})
			var stand string
			must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
			worker := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Ada", LastName: "Helferin"}).ID
			exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: worker})
			path := "/shifts/" + shift
			request := func(method, target string, values url.Values) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				web.ServeHTTP(w, r)
				return w
			}
			edit := request(http.MethodGet, path+"/edit", nil)
			if edit.Code != http.StatusOK {
				t.Fatal("edit form unavailable", edit.Code)
			}
			proposal := shiftFormInputs(t, edit, path)
			proposal.Set("title", "Mein erhaltener Vorschlag")
			proposal.Set("capacity", "3")
			if proposal.Get("version") != "1" || proposal.Has("reconcile_version") {
				t.Fatal("unexpected initial edit revision", proposal)
			}
			concurrent := func(title, start, end string, capacity, version int) {
				exec(t, app, s.Command{Action: "shift-save", ID: shift, Title: title, Start: start, End: end, Capacity: capacity, Version: version, Confirm: true})
			}
			concurrent("Gespeicherter Parallelstand", "2027-09-10T10:00:00+02:00", "2027-09-10T13:00:00+02:00", 2, 1)
			checkConflict := func(w *httptest.ResponseRecorder, revision, title, clock, capacity string) url.Values {
				t.Helper()
				body := w.Body.String()
				if w.Code != http.StatusConflict || strings.Contains(strings.ToLower(body), "<!doctype html>") == hx || strings.Count(body, `id="app-frame"`) != 1 {
					t.Fatalf("wrong conflict response: %d %s", w.Code, body)
				}
				for _, current := range []string{"<dd>" + title + "</dd>", clock, "<dd>" + capacity + "</dd>", `id="shift-reconcile_confirm"`, `name="reconcile_confirm" value="true" required`} {
					if !strings.Contains(body, current) {
						t.Fatal("current values or explicit acknowledgement missing", current, body)
					}
				}
				values := shiftFormInputs(t, w, path)
				if values.Get("version") != "1" || values.Get("reconcile_version") != revision || values.Has("reconcile_confirm") || values.Get("title") != "Mein erhaltener Vorschlag" || values.Get("start") != "2027-09-10T09:00" || values.Get("end") != "2027-09-10T12:00" || values.Get("capacity") != "3" {
					t.Fatal("proposal/revisions lost or acknowledgement preselected", values)
				}
				return values
			}
			conflict := request(http.MethodPost, path, proposal)
			retry := checkConflict(conflict, "2", "Gespeicherter Parallelstand", "10.09.2027 10:00 CEST +0200", "2")
			unconfirmed := request(http.MethodPost, path, retry)
			retry = checkConflict(unconfirmed, "2", "Gespeicherter Parallelstand", "10.09.2027 10:00 CEST +0200", "2")
			if count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND version=2 AND title='Gespeicherter Parallelstand'", shift) != 1 {
				t.Fatal("missing acknowledgement was not enforced on the server")
			}
			assertShiftReconciliationErrorLink(t, unconfirmed)
			// Reconciliation never substitutes for the existing staffed-time confirmation.
			retry.Set("reconcile_confirm", "true")
			retry = checkConflict(request(http.MethodPost, path, retry), "2", "Gespeicherter Parallelstand", "10.09.2027 10:00 CEST +0200", "2")
			if count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND version=2 AND title='Gespeicherter Parallelstand'", shift) != 1 {
				t.Fatal("reconciliation bypassed staffed-time confirmation")
			}
			// A second edit invalidates the exact comparison the user acknowledged.
			retry.Set("reconcile_confirm", "true")
			retry.Set("confirm", "true")
			concurrent("Weiterer Parallelstand", "2027-09-10T11:00:00+02:00", "2027-09-10T14:00:00+02:00", 4, 2)
			retry = checkConflict(request(http.MethodPost, path, retry), "3", "Weiterer Parallelstand", "10.09.2027 11:00 CEST +0200", "4")
			retry = checkConflict(request(http.MethodPost, path, retry), "3", "Weiterer Parallelstand", "10.09.2027 11:00 CEST +0200", "4")
			if count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND version=3 AND title='Weiterer Parallelstand'", shift) != 1 {
				t.Fatal("intervening edit was overwritten before acknowledgement")
			}
			// Validation must also retain the comparison and require a new acknowledgement.
			retry.Set("reconcile_confirm", "true")
			retry.Set("capacity", "0")
			invalid := request(http.MethodPost, path, retry)
			if invalid.Code != http.StatusUnprocessableEntity {
				t.Fatal("invalid proposal accepted", invalid.Code)
			}
			retry = shiftFormInputs(t, invalid, path)
			if retry.Get("reconcile_version") != "3" || retry.Get("capacity") != "0" || retry.Has("reconcile_confirm") {
				t.Fatal("validation lost comparison/proposal or retained acknowledgement", retry)
			}
			retry.Set("capacity", "3")
			retry.Set("reconcile_confirm", "true")
			retry.Set("confirm", "true")
			checkSuccess := func(w *httptest.ResponseRecorder) {
				t.Helper()
				if hx {
					if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("HX-Replace-Url"), path+"?") || strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
						t.Fatal("successful frame retry failed", w.Code, w.Header())
					}
				} else if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), path+"?") {
					t.Fatal("successful full retry failed", w.Code, w.Header())
				}
			}
			checkSuccess(request(http.MethodPost, path, retry))
			if count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND title='Mein erhaltener Vorschlag' AND capacity=3 AND version=4 AND starts_at='2027-09-10T09:00:00+02:00' AND ends_at='2027-09-10T12:00:00+02:00'", shift) != 1 {
				t.Fatal("deliberate retry did not save exactly the proposal")
			}
			// Labels/capacity still share the existing time revision.
			labelOnly := shiftFormInputs(t, request(http.MethodGet, path+"/edit", nil), path)
			labelOnly.Set("title", "Nur Bezeichnung und Plätze")
			labelOnly.Set("capacity", "5")
			checkSuccess(request(http.MethodPost, path, labelOnly))
			if count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND version=4 AND capacity=5 AND title='Nur Bezeichnung und Plätze'", shift) != 1 {
				t.Fatal("label/capacity edit changed time-revision semantics")
			}
			// Even a valid acknowledgement must freshly authorize the eventual write.
			concurrent("Nachfolgend gespeichert", "2027-09-10T10:00:00+02:00", "2027-09-10T13:00:00+02:00", 5, 4)
			stale := request(http.MethodPost, path, labelOnly)
			if stale.Code != http.StatusConflict {
				t.Fatal("expected conflict before revocation", stale.Code)
			}
			revoked := shiftFormInputs(t, stale, path)
			revoked.Set("reconcile_confirm", "true")
			revoked.Set("confirm", "true")
			exec(t, app, s.Command{Action: "team-remove", ParticipationID: part, AccountID: account})
			denied := request(http.MethodPost, path, revoked)
			if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), "Nachfolgend gespeichert") || count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND version=5 AND title='Nachfolgend gespeichert'", shift) != 1 {
				t.Fatal("reconciled retry bypassed fresh authorization", denied.Code)
			}
		})
	}
}

func assertShiftReconciliationErrorLink(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	must(t, err)
	var summaryTarget, labelTarget string
	var control map[string]string
	ids := map[string]bool{}
	var visit func(*html.Node, bool)
	visit = func(n *html.Node, inSummary bool) {
		attrs := map[string]string{}
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		if attrs["id"] != "" {
			ids[attrs["id"]] = true
		}
		inSummary = inSummary || attrs["id"] == "form-errors"
		if inSummary && n.Data == "a" {
			summaryTarget = strings.TrimPrefix(attrs["href"], "#")
		}
		if n.Data == "input" && attrs["name"] == "reconcile_confirm" {
			control = attrs
		}
		if n.Data == "label" && attrs["for"] == "shift-reconcile_confirm" {
			labelTarget = attrs["for"]
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, inSummary)
		}
	}
	visit(doc, false)
	if summaryTarget == "" || control["id"] != summaryTarget || labelTarget != control["id"] || control["aria-invalid"] != "true" || !ids[summaryTarget+"-error"] || !strings.Contains(" "+control["aria-describedby"]+" ", " "+summaryTarget+"-error ") {
		t.Fatal("error summary/label does not target the invalid reconciliation control and its description", summaryTarget, labelTarget, control)
	}
}

// Read the returned native form as a browser would, excluding unchecked controls.
func shiftFormInputs(t *testing.T, response *httptest.ResponseRecorder, action string) url.Values {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	must(t, err)
	var form *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, a := range n.Attr {
				if a.Key == "action" && a.Val == action {
					form = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if form == nil {
		t.Fatal("shift edit form missing", response.Code, response.Body.String())
	}
	values := url.Values{}
	var read func(*html.Node)
	read = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			_, checked := attrs["checked"]
			if attrs["name"] != "" && (attrs["type"] != "checkbox" && attrs["type"] != "radio" || checked) {
				values.Add(attrs["name"], attrs["value"])
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			read(c)
		}
	}
	read(form)
	return values
}
