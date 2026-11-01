package scheduling_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

func retainedShiftEdit(id string) s.Command {
	return s.Command{Action: "shift-save", ID: id, Title: "Aufbau", Start: "2027-09-10T09:00:00+02:00", End: "2027-09-10T12:00:00+02:00", Capacity: 2, Version: 1}
}

func correctRetainedShiftDates(t *testing.T, app *s.Store, edition string) {
	t.Helper()
	exec(t, app, s.Command{Action: "edition-save", ID: edition, Label: "Corrected dates", StartDay: "2027-12-10", EndDay: "2027-12-12"})
}

// Include every table, including the authorization revision and audit, so a
// rejected Execute cannot hide a partial write outside the shift itself.
func retainedShiftSnapshot(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	var tables []string
	must(t, db.Raw("SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename").Scan(&tables).Error)
	out := make(map[string]string, len(tables))
	for _, table := range tables {
		var rows string
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		must(t, db.Raw("SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text), '[]'::jsonb)::text FROM "+quoted+" r").Scan(&rows).Error)
		out[table] = rows
	}
	return out
}

func rejectRetainedShiftEdit(t *testing.T, app *s.Store, db *gorm.DB, who s.Identity, c s.Command, want error) {
	t.Helper()
	before := retainedShiftSnapshot(t, db)
	if _, err := app.Execute(ctx, who, c); !errors.Is(err, want) {
		t.Fatalf("Execute(%s) = %v, want %v", c.Action, err, want)
	}
	if after := retainedShiftSnapshot(t, db); !reflect.DeepEqual(before, after) {
		for table := range before {
			if before[table] != after[table] {
				t.Errorf("rejected command changed table %s", table)
			}
		}
	}
}

func TestRetainedShiftMetadataEditsPreserveRequestVersions(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	c := retainedShiftEdit(shift)
	exec(t, app, c)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	var requests []string
	var pending s.Submission
	for _, first := range []string{"Assigned", "Pending"} {
		sub, err := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: first, LastName: "Retained", ShiftIDs: []string{shift}})
		must(t, err)
		var request string
		must(t, db.Raw("SELECT id FROM requests WHERE worker_id=?", sub.WorkerID).Scan(&request).Error)
		requests = append(requests, request)
		pending = sub
	}
	exec(t, app, s.Command{Action: "approve", ID: requests[0]})
	who := s.Identity{Issuer: "test:issuer", Subject: "retained-pending"}
	_, err := app.Execute(ctx, who, s.Command{Action: "connect", Token: pending.ConnectionToken})
	must(t, err)
	correctRetainedShiftDates(t, app, edition)
	before := retainedShiftSnapshot(t, db)
	var original s.Shift
	must(t, db.Raw("SELECT * FROM shifts WHERE id=?", shift).Scan(&original).Error)

	// A title change and both directions of capacity change keep the old times.
	c.Title = "Renamed retained shift"
	exec(t, app, c)
	c.Capacity = 3
	exec(t, app, c)
	c.Capacity = 1
	// Equivalent instants in another offset are still unchanged times.
	c.Start, c.End = "2027-09-10T07:00:00Z", "2027-09-10T10:00:00Z"
	exec(t, app, c)
	var updated s.Shift
	must(t, db.Raw("SELECT * FROM shifts WHERE id=?", shift).Scan(&updated).Error)
	if updated.ID != original.ID || updated.ParticipationID != part || updated.Title != c.Title || updated.Capacity != 1 || updated.Version != original.Version || !updated.StartsAt.Equal(original.StartsAt) || !updated.EndsAt.Equal(original.EndsAt) || updated.Cancelled {
		t.Fatalf("metadata edit changed shift identity, time, or version: %+v", updated)
	}
	view, err := app.View(ctx, admin, edition)
	must(t, err)
	if len(view.Schedules) != 1 || len(view.Schedules[0].Shifts) != 1 || !view.Schedules[0].Shifts[0].Outside {
		t.Fatal("metadata edit cleared the retained shift's outside-window warning")
	}
	after := retainedShiftSnapshot(t, db)
	for _, table := range []string{"shifts", "scheduling_state", "scheduling_audit"} {
		delete(before, table)
		delete(after, table)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("metadata edits changed related records")
	}
	own, err := app.OwnRequests(ctx, who, "", "", 1)
	must(t, err)
	if len(own.Rows) != 1 || own.Rows[0].Stale || own.Rows[0].ShiftVersion != 1 || own.Rows[0].Status != "pending" {
		t.Fatalf("metadata edit changed request status or freshness: %+v", own.Rows)
	}
	rejectRetainedShiftEdit(t, app, db, admin, s.Command{Action: "approve", ID: requests[1]}, s.ErrConflict)
	c.Capacity = 2
	exec(t, app, c)
	exec(t, app, s.Command{Action: "approve", ID: requests[1]})
	if count(t, db, "SELECT count(*) FROM assignments WHERE shift_id=? AND active", shift) != 2 {
		t.Fatal("retained shift did not retain and accept assignments within capacity")
	}

	c.Start, c.End = "2027-12-10T09:00:00+01:00", "2027-12-10T12:00:00+01:00"
	rejectRetainedShiftEdit(t, app, db, admin, c, s.ErrConflict)
	c.Confirm = true
	exec(t, app, c)
	must(t, db.Raw("SELECT * FROM shifts WHERE id=?", shift).Scan(&updated).Error)
	var corrected s.Edition
	must(t, db.Raw("SELECT * FROM editions WHERE id=?", edition).Scan(&corrected).Error)
	if updated.Version != 2 || !updated.Within(corrected) || count(t, db, "SELECT count(*) FROM assignments WHERE shift_id=? AND active", shift) != 2 {
		t.Fatal("confirmed time edit did not increment version and preserve assignments")
	}
	own, err = app.OwnRequests(ctx, who, "", "", 1)
	must(t, err)
	if len(own.Rows) != 1 || !own.Rows[0].Stale || own.Rows[0].ShiftVersion != 1 || own.Rows[0].CurrentVersion != 2 || own.Rows[0].Status != "approved" {
		t.Fatalf("time edit did not retain the original request version: %+v", own.Rows)
	}
	// The old version still cannot overwrite the now valid shift.
	c.Title = "Stale overwrite"
	rejectRetainedShiftEdit(t, app, db, admin, c, s.ErrConflict)
}

func TestRetainedShiftEditFailuresAreAtomic(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	c := retainedShiftEdit(shift)
	exec(t, app, c)
	var stand string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	for _, first := range []string{"First", "Second"} {
		worker := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: first, LastName: "Assigned"}).ID
		exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: worker})
	}
	correctRetainedShiftDates(t, app, edition)
	for _, tc := range []struct {
		name   string
		change func(*s.Command)
		want   error
	}{
		{"missing version", func(c *s.Command) { c.Version = 0 }, s.ErrConflict},
		{"wrong version", func(c *s.Command) { c.Version = 2 }, s.ErrConflict},
		{"zero capacity", func(c *s.Command) { c.Capacity = 0 }, s.ErrInvalid},
		{"below assigned capacity", func(c *s.Command) { c.Capacity = 1 }, s.ErrConflict},
		{"empty title", func(c *s.Command) { c.Title = " " }, s.ErrInvalid},
		{"invalid start", func(c *s.Command) { c.Start = "invalid" }, s.ErrInvalid},
		{"invalid end", func(c *s.Command) { c.End = "invalid" }, s.ErrInvalid},
		{"equal times", func(c *s.Command) { c.End = c.Start }, s.ErrInvalid},
		{"reversed times", func(c *s.Command) { c.Start, c.End = c.End, c.Start }, s.ErrInvalid},
		{"changed start outside window", func(c *s.Command) { c.Start = "2027-09-10T10:00:00+02:00"; c.Confirm = true }, s.ErrInvalid},
		{"changed end outside window", func(c *s.Command) { c.End = "2027-09-10T13:00:00+02:00"; c.Confirm = true }, s.ErrInvalid},
		{"new shift outside window", func(c *s.Command) { c.ID = ""; c.ParticipationID = part }, s.ErrInvalid},
		{"staffed move needs confirmation", func(c *s.Command) { c.Start, c.End = "2027-12-10T09:00:00+01:00", "2027-12-10T12:00:00+01:00" }, s.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edit := c
			tc.change(&edit)
			rejectRetainedShiftEdit(t, app, db, admin, edit, tc.want)
		})
	}
	exec(t, app, s.Command{Action: "shift-cancel", ID: shift, Confirm: true})
	c.Version = 2
	rejectRetainedShiftEdit(t, app, db, admin, c, s.ErrConflict)
}

func TestRetainedShiftMetadataEditsRespectParticipationScope(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	team := s.Identity{Issuer: "test:issuer", Subject: "retained-team"}
	invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part}).Token
	_, err := app.Execute(ctx, team, s.Command{Action: "redeem-organizer", Token: invite})
	must(t, err)
	correctRetainedShiftDates(t, app, edition)
	c := retainedShiftEdit(shift)
	c.Title = "Current team edit"
	_, err = app.Execute(ctx, team, c)
	must(t, err)
	rejectRetainedShiftEdit(t, app, db, s.Identity{}, c, s.ErrDenied)
	rejectRetainedShiftEdit(t, app, db, s.Identity{Issuer: "test:issuer", Subject: "unrelated"}, c, s.ErrDenied)

	next := exec(t, app, s.Command{Action: "edition-save", Label: "Next edition", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	exec(t, app, s.Command{Action: "activate", ID: next, Confirm: true})
	var nextPart string
	must(t, db.Raw("SELECT id FROM participations WHERE edition_id=? AND stand_id=(SELECT stand_id FROM participations WHERE id=?)", next, part).Scan(&nextPart).Error)
	// Client-supplied scope cannot make an archived shift current.
	c.ParticipationID, c.EditionID = nextPart, next
	rejectRetainedShiftEdit(t, app, db, team, c, s.ErrDenied)
	c.Title = "Archived global team edit"
	exec(t, app, c)
	if count(t, db, "SELECT count(*) FROM shifts WHERE participation_id=?", nextPart) != 0 || count(t, db, "SELECT count(*) FROM shifts WHERE id=? AND participation_id=? AND title=?", shift, part, c.Title) != 1 {
		t.Fatal("metadata edit crossed edition ownership")
	}
	// A new shift resolves its actual participation's dates, ignoring EditionID.
	newShift := c
	newShift.ID, newShift.EditionID = "", edition
	rejectRetainedShiftEdit(t, app, db, admin, newShift, s.ErrInvalid)
	view, err := app.View(ctx, admin, edition)
	must(t, err)
	exec(t, app, s.Command{Action: "team-remove", ParticipationID: part, AccountID: view.AccountID})
	rejectRetainedShiftEdit(t, app, db, admin, c, s.ErrDenied)
	exec(t, app, s.Command{Action: "team-add", ParticipationID: part, AccountID: view.AccountID})
	exec(t, app, s.Command{Action: "participation", ID: part, Enabled: false})
	rejectRetainedShiftEdit(t, app, db, admin, c, s.ErrDenied)
}
