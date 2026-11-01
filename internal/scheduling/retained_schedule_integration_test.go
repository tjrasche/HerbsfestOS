package scheduling_test

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
)

func TestRetainedScheduleAndDetailAfterEditionDateCorrection(t *testing.T) {
	for _, correction := range []struct{ start, end string }{{"2027-12-10", "2027-12-12"}, {"2027-06-10", "2027-06-12"}} {
		t.Run(correction.start, func(t *testing.T) {
			app, db := setup(t)
			edition, part, shift := baseline(t, app)
			// The second Berlin day starts on the preceding UTC date.
			night := exec(t, app, s.Command{Action: "shift-save", ParticipationID: part, Title: "Nachtdienst", Start: "2027-09-10T21:30:00Z", End: "2027-09-10T22:30:00Z", Capacity: 2}).ID
			local := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
			invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part}).Token
			_, err := app.Execute(ctx, local, s.Command{Action: "redeem-organizer", Token: invite})
			must(t, err)
			var before []s.Shift
			must(t, db.Raw("SELECT * FROM shifts ORDER BY id").Scan(&before).Error)
			exec(t, app, s.Command{Action: "edition-save", ID: edition, Label: "Korrigierte Ausgabe", StartDay: correction.start, EndDay: correction.end})

			for _, filter := range []s.ScheduleFilter{{}, {Stand: part, Day: "2027-09-10"}} {
				plan, err := app.ScheduleSummary(ctx, local, filter)
				must(t, err)
				if plan.Filter.Day != "2027-09-10" || len(plan.Shifts) != 2 || !plan.Shifts[0].Outside || !plan.Shifts[1].Outside || !plan.Shifts[0].CanEdit {
					t.Fatalf("retained default/filtered plan unavailable: %+v", plan)
				}
			}
			plan, err := app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: part, Day: "2027-09-11"})
			must(t, err)
			if len(plan.Shifts) != 1 || plan.Shifts[0].ID != night || !plan.Shifts[0].Outside || !plan.Shifts[0].Continuation || plan.Shifts[0].Top != 0 {
				t.Fatalf("retained Berlin continuation lost: %+v", plan.Shifts)
			}
			detail, err := app.ShiftDetail(ctx, local, shift, s.ScheduleFilter{})
			must(t, err)
			if !detail.Shift.Outside || !detail.CanEdit || detail.Schedule.Filter.Day != "2027-09-10" || !detail.Schedule.Shifts[0].Selected {
				t.Fatalf("retained direct detail lost: %+v", detail)
			}
			plan, err = app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: part, Day: "2027-09-10", Page: 2})
			must(t, err)
			if len(plan.Shifts) != 0 || plan.Previous == "" || plan.HasNext {
				t.Fatalf("retained day validity depends on page contents: %+v", plan)
			}
			plan, err = app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: part, Day: correction.start})
			must(t, err)
			if len(plan.Shifts) != 0 {
				t.Fatal("empty day in corrected window contains retained shifts")
			}
			for _, day := range []string{"2027-09-12", "2027-02-30", "2027-9-10", "2027-09-10T00:00:00+02:00"} {
				if _, err = app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: part, Day: day}); !errors.Is(err, s.ErrInvalid) {
					t.Fatalf("empty outside day or malformed day %q accepted: %v", day, err)
				}
			}

			web := uiHTTP(t, app, func() {})
			for _, path := range []string{"/", "/?stand=" + part + "&day=2027-09-10", "/?stand=" + part + "&day=2027-09-11", "/shifts/" + shift, "/shifts/" + night + "?day=2027-09-11"} {
				for _, hx := range []bool{false, true} {
					r := httptest.NewRequest("GET", path, nil)
					if hx {
						r.Header.Set("HX-Request", "true")
					}
					w := httptest.NewRecorder()
					web.ServeHTTP(w, r)
					body := w.Body.String()
					if w.Code != 200 || !strings.Contains(body, "Zeiten prüfen") || !strings.Contains(body, `data-shift-id="`) || strings.Count(body, `id="app-frame"`) != 1 {
						t.Fatalf("retained route %s (HX=%t): %d %s", path, hx, w.Code, body)
					}
				}
			}
			for _, day := range []string{"2027-09-12", "2027-02-30"} {
				w := httptest.NewRecorder()
				web.ServeHTTP(w, httptest.NewRequest("GET", "/?stand="+part+"&day="+day, nil))
				if w.Code != 422 || strings.Contains(w.Body.String(), "Nachtdienst") {
					t.Fatalf("invalid day route %s: %d", day, w.Code)
				}
			}
			var after []s.Shift
			must(t, db.Raw("SELECT * FROM shifts ORDER BY id").Scan(&after).Error)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("edition correction or reads changed retained shifts")
			}
		})
	}
}

func TestRetainedScheduleKeepsPermissionsAndEditionScope(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	var stand, adminAccount string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	must(t, db.Raw("SELECT id FROM accounts WHERE issuer=? AND subject=?", admin.Issuer, admin.Subject).Scan(&adminAccount).Error)
	otherStand := exec(t, app, s.Command{Action: "stand-save", Name: "Verborgener Stand"}).ID
	otherPart := exec(t, app, s.Command{Action: "participation", EditionID: edition, StandID: otherStand}).ID
	exec(t, app, s.Command{Action: "team-add", ParticipationID: otherPart, AccountID: adminAccount})
	exec(t, app, s.Command{Action: "participation", ID: otherPart, Enabled: true})
	otherShift := exec(t, app, s.Command{Action: "shift-save", ParticipationID: otherPart, Title: "Verborgener Dienst", Start: "2027-09-12T09:00:00+02:00", End: "2027-09-12T12:00:00+02:00", Capacity: 1}).ID
	otherEdition := exec(t, app, s.Command{Action: "edition-save", Label: "Andere Ausgabe", StartDay: "2027-09-13", EndDay: "2027-09-15", SourceID: edition}).ID
	var archivePart string
	must(t, db.Raw("SELECT id FROM participations WHERE edition_id=? AND stand_id=?", otherEdition, stand).Scan(&archivePart).Error)
	archiveShift := exec(t, app, s.Command{Action: "shift-save", ParticipationID: archivePart, Title: "Anderer Ausgabedienst", Start: "2027-09-14T09:00:00+02:00", End: "2027-09-14T12:00:00+02:00", Capacity: 1}).ID

	// A read-only member can view this stand, but cannot see pending identities.
	member := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Plan", LastName: "Leserin"}).ID
	local := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
	link := exec(t, app, s.Command{Action: "connection-link", WorkerID: member}).Token
	_, err := app.Execute(ctx, local, s.Command{Action: "connect", Token: link})
	must(t, err)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	_, err = app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Private", LastName: "Anfrage", ShiftIDs: []string{shift}})
	must(t, err)
	confirmed := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Sichtbare", LastName: "Einteilung"}).ID
	exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: confirmed})
	exec(t, app, s.Command{Action: "edition-save", ID: edition, Label: "Korrigierte Ausgabe", StartDay: "2027-12-10", EndDay: "2027-12-12"})

	plan, err := app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: "all", Day: "2027-09-10"})
	must(t, err)
	if len(plan.Shifts) != 1 || plan.Shifts[0].ID != shift || !plan.Shifts[0].Outside || plan.Shifts[0].CanEdit || plan.Pending != 0 || plan.Shifts[0].Pending != 0 || plan.Shifts[0].Confirmed != 1 {
		t.Fatalf("retained member summary leaked permissions or counts: %+v", plan)
	}
	detail, err := app.ShiftDetail(ctx, local, shift, s.ScheduleFilter{})
	must(t, err)
	if !detail.Shift.Outside || detail.CanEdit || detail.CanManageMembers || len(detail.Shift.Pending) != 0 || len(detail.Shift.Roster) != 1 || detail.Shift.Roster[0].Name != "Sichtbare Einteilung" {
		t.Fatalf("retained member detail leaked permissions or people: %+v", detail)
	}
	for _, filter := range []s.ScheduleFilter{{Stand: part, Day: "2027-09-12"}, {Stand: "all", Day: "2027-09-12"}, {Stand: part, Day: "2027-09-14"}} {
		if _, err = app.ScheduleSummary(ctx, local, filter); !errors.Is(err, s.ErrInvalid) {
			t.Fatalf("another resource admitted outside day %+v: %v", filter, err)
		}
	}
	if _, err = app.ScheduleSummary(ctx, local, s.ScheduleFilter{Stand: otherPart, Day: "2027-09-12"}); !errors.Is(err, s.ErrDenied) {
		t.Fatalf("hidden stand filter allowed: %v", err)
	}
	for _, id := range []string{otherShift, archiveShift} {
		if _, err = app.ShiftDetail(ctx, local, id, s.ScheduleFilter{Stand: part, Day: "2027-09-10"}); !errors.Is(err, s.ErrDenied) {
			t.Fatalf("forged filter allowed actual shift %s: %v", id, err)
		}
	}
	// Even a global reader must select the edition that owns the shift.
	if _, err = app.ShiftDetail(ctx, admin, archiveShift, s.ScheduleFilter{}); !errors.Is(err, s.ErrDenied) {
		t.Fatalf("global detail crossed the selected edition: %v", err)
	}

	web := uiHTTP(t, app, func() {})
	for _, tc := range []struct {
		path string
		code int
	}{{"/", 200}, {"/?stand=all&day=2027-09-10", 200}, {"/shifts/" + shift, 200}, {"/?stand=all&day=2027-09-12", 422}, {"/?stand=" + otherPart + "&day=2027-09-12", 403}, {"/shifts/" + otherShift + "?stand=" + part + "&day=2027-09-10", 403}, {"/shifts/" + archiveShift + "?stand=" + part, 403}, {"/?edition=" + otherEdition + "&stand=" + archivePart + "&day=2027-09-14", 403}} {
		w := httptest.NewRecorder()
		web.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		body := w.Body.String()
		if w.Code != tc.code || strings.Contains(body, "Private Anfrage") || strings.Contains(body, "offene Anfragen") || strings.Contains(body, `href="/shifts/`+shift+`/edit`) || strings.Contains(body, "Verborgener") || strings.Contains(body, "Anderer Ausgabedienst") {
			t.Fatalf("retained member route %s: %d %s", tc.path, w.Code, body)
		}
		if tc.code == 200 && !strings.Contains(body, "Zeiten prüfen") {
			t.Fatalf("retained member route lost outside marker: %s", tc.path)
		}
	}

	exec(t, app, s.Command{Action: "member-remove", StandID: stand, WorkerID: member})
	if _, err = app.ShiftDetail(ctx, local, shift, s.ScheduleFilter{}); !errors.Is(err, s.ErrDenied) {
		t.Fatalf("revoked membership retained shift access: %v", err)
	}
	w := httptest.NewRecorder()
	web.ServeHTTP(w, httptest.NewRequest("GET", "/shifts/"+shift, nil))
	if w.Code != 403 || strings.Contains(w.Body.String(), "Sichtbare Einteilung") {
		t.Fatalf("retained detail ignored revoked membership: %d", w.Code)
	}
}
