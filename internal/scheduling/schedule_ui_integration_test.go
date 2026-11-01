package scheduling_test

import (
	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestShiftUIValidationAndConflictKeepResourceInputs(t *testing.T) {
	app, db := setup(t)
	_, part, shift := baseline(t, app)
	web := uiHTTP(t, app, func() {
		must(t, s.Bootstrap(ctx, db, "urn:herbstfest:local", "developer"))
		var account string
		must(t, db.Raw("SELECT id FROM accounts WHERE issuer='urn:herbstfest:local' AND subject='developer'").Scan(&account).Error)
		exec(t, app, s.Command{Action: "team-add", ParticipationID: part, AccountID: account})
	})
	for _, tc := range []struct {
		capacity, version string
		want              int
	}{{"0", "1", 422}, {"9", "0", 409}} {
		values := url.Values{"title": {"Erhaltene Dienstbezeichnung"}, "capacity": {tc.capacity}, "version": {tc.version}, "start": {"2027-09-10T10:00"}, "end": {"2027-09-10T13:00"}, "return_to": {"/?stand=" + part + "&day=2027-09-10"}}
		r := httptest.NewRequest("POST", "/shifts/"+shift, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		web.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != tc.want || !strings.Contains(body, "Erhaltene Dienstbezeichnung") || !strings.Contains(body, `value="2027-09-10T10:00"`) || !strings.Contains(body, `value="`+tc.capacity+`"`) || !strings.Contains(body, `action="/shifts/`+shift+`"`) || strings.Contains(strings.ToLower(body), "<!doctype html>") {
			t.Fatalf("values or frame lost on %d: %d %s", tc.want, w.Code, body)
		}
	}
	if count(t, db, "SELECT count(*) FROM shifts WHERE title='Erhaltene Dienstbezeichnung'") != 0 {
		t.Fatal("invalid or stale form changed shift")
	}
}
func TestScheduleDayIncludesOvernightContinuation(t *testing.T) {
	app, _ := setup(t)
	_, part, _ := baseline(t, app)
	id := exec(t, app, s.Command{Action: "shift-save", ParticipationID: part, Title: "Über Nacht", Start: "2027-09-10T23:00:00+02:00", End: "2027-09-11T02:00:00+02:00", Capacity: 2}).ID
	summary, err := app.ScheduleSummary(ctx, admin, s.ScheduleFilter{Stand: part, Day: "2027-09-11"})
	must(t, err)
	if len(summary.Shifts) != 1 || summary.Shifts[0].ID != id || summary.Shifts[0].Top != 0 || summary.Shifts[0].Height != 120 {
		t.Fatalf("overnight continuation missing: %+v", summary.Shifts)
	}
}

func TestPendingQueueRecoversWhenLastRowOnPageIsDecided(t *testing.T) {
	app, db := setup(t)
	_, part, shift := baseline(t, app)
	for i := 0; i < 21; i++ {
		id := s.ID()
		must(t, db.Exec("INSERT INTO workers(id,first_name,last_name) VALUES(?,'Queue','Person')", id).Error)
		must(t, db.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version) VALUES(?,?,?,'pending',1)", s.ID(), shift, id).Error)
	}
	filter := s.ScheduleFilter{Stand: part, Day: "2027-09-10", Page: 2}
	page, err := app.PendingRequests(ctx, admin, filter)
	must(t, err)
	if len(page.Rows) != 1 {
		t.Fatal("expected one final row", len(page.Rows))
	}
	exec(t, app, s.Command{Action: "decline", ID: page.Rows[0].ID})
	page, err = app.PendingRequests(ctx, admin, filter)
	must(t, err)
	if page.Filter.Page != 1 || len(page.Rows) != 20 || page.HasNext {
		t.Fatalf("queue did not recover: page=%d rows=%d next=%t", page.Filter.Page, len(page.Rows), page.HasNext)
	}
}
