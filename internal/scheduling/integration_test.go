package scheduling_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/database"
	"github.com/tjrasche/HerbsfestOS/internal/openfga"
	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

var ctx = context.Background()
var admin = s.Identity{Issuer: "test:issuer", Subject: "admin"}

func setup(t *testing.T) (*s.Store, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("SCHEDULING_TEST_DATABASE_URL")
	base := os.Getenv("SCHEDULING_TEST_FGA_URL")
	if dsn == "" || base == "" {
		t.Skip("set disposable SCHEDULING_TEST_DATABASE_URL and SCHEDULING_TEST_FGA_URL")
	}
	db, pool, e := database.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "test_" + strings.ToLower(s.ID())
	must(t, db.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE"); pool.Close() })
	scoped, p, e := database.Open(ctx, dsn+"&search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close() })
	must(t, s.Migrate(ctx, scoped, s.LegacyEdition{}))
	must(t, s.Bootstrap(ctx, scoped, admin.Issuer, admin.Subject))
	post := func(path string, v any) map[string]any {
		b, _ := json.Marshal(v)
		r, e := http.Post(base+path, "application/json", strings.NewReader(string(b)))
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var out map[string]any
		json.NewDecoder(r.Body).Decode(&out)
		if r.StatusCode >= 300 {
			t.Fatalf("engine %d %v", r.StatusCode, out)
		}
		return out
	}
	store := post("/stores", map[string]string{"name": "application-integration"})["id"].(string)
	model, _ := s.Files.ReadFile("authorization-model.json")
	var m any
	json.Unmarshal(model, &m)
	mid := post("/stores/"+store+"/authorization-models", m)["authorization_model_id"].(string)
	fga, e := openfga.New(openfga.Config{APIURL: base, StoreID: store, AuthorizationModelID: mid, Timeout: time.Second * 3})
	must(t, e)
	t.Cleanup(fga.Close)
	app := s.New(scoped, fga)
	app.Now = func() time.Time { return time.Date(2027, 9, 1, 0, 0, 0, 0, s.Berlin) }
	return app, scoped
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func exec(t *testing.T, app *s.Store, c s.Command) s.Result {
	t.Helper()
	v, e := app.Execute(ctx, admin, c)
	must(t, e)
	return v
}
func baseline(t *testing.T, app *s.Store) (string, string, string) {
	edition := exec(t, app, s.Command{Action: "edition-save", Label: "2027", StartDay: "2027-09-10", EndDay: "2027-09-12"}).ID
	stand := exec(t, app, s.Command{Action: "stand-save", Name: "Bierstand"}).ID
	part := exec(t, app, s.Command{Action: "participation", EditionID: edition, StandID: stand, Enabled: true}).ID
	v, e := app.View(ctx, admin, "")
	must(t, e)
	exec(t, app, s.Command{Action: "team-add", ParticipationID: part, AccountID: v.AccountID})
	exec(t, app, s.Command{Action: "activate", ID: edition, Confirm: true})
	shift := exec(t, app, s.Command{Action: "shift-save", ParticipationID: part, Title: "Aufbau", Start: "2027-09-10T09:00:00+02:00", End: "2027-09-10T12:00:00+02:00", Capacity: 1}).ID
	return edition, part, shift
}
func count(t *testing.T, db *gorm.DB, q string, args ...any) int {
	t.Helper()
	var n int
	must(t, db.Raw(q, args...).Scan(&n).Error)
	return n
}
func TestApplicationCapacityAtomicityAndRetry(t *testing.T) {
	app, db := setup(t)
	_, part, shift := baseline(t, app)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	input := s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Anna", LastName: "Gast", ShiftIDs: []string{shift}}
	one, e := app.Submit(ctx, s.Identity{}, input)
	must(t, e)
	again, e := app.Submit(ctx, s.Identity{}, input)
	must(t, e)
	if one != again {
		t.Fatal("retry changed result")
	}
	input.Key = s.ID() + s.ID()
	input.FirstName = "Berta"
	two, e := app.Submit(ctx, s.Identity{}, input)
	must(t, e)
	var requests []s.Request
	must(t, db.Raw("SELECT * FROM requests ORDER BY id").Scan(&requests).Error)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, r := range requests {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, e := app.Execute(ctx, admin, s.Command{Action: "approve", ID: id})
			results <- e
		}(r.ID)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, s.ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 || count(t, db, "SELECT count(*) FROM assignments WHERE active") != 1 || count(t, db, "SELECT count(*) FROM memberships") != 1 {
		t.Fatal("capacity/membership race failed")
	}
	if one.WorkerID == two.WorkerID {
		t.Fatal("names merged automatically")
	}
	before := count(t, db, "SELECT count(*) FROM workers")
	input.Key = s.ID() + s.ID()
	input.ShiftIDs = []string{shift, "missing"}
	if _, e = app.Submit(ctx, s.Identity{}, input); e == nil {
		t.Fatal("invalid atomic batch accepted")
	}
	if count(t, db, "SELECT count(*) FROM workers") != before {
		t.Fatal("partial worker write")
	}
}
func TestApplicationPermissionsLinkingAndTransitions(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	v, e := app.View(ctx, admin, "")
	must(t, e)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	submission, e := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Private", LastName: "Person", ShiftIDs: []string{shift}})
	must(t, e)
	worker := s.Identity{Issuer: "test:issuer", Subject: "worker"}
	_, e = app.Execute(ctx, worker, s.Command{Action: "connect", Token: submission.ConnectionToken})
	must(t, e)
	wv, e := app.View(ctx, worker, "")
	must(t, e)
	if len(wv.Schedules) != 0 || len(wv.OwnRequests) != 1 {
		t.Fatal("pending request leaked roster")
	}
	if _, e = app.Directory(ctx, worker, "Private"); !errors.Is(e, s.ErrDenied) {
		t.Fatal("worker directory allowed", e)
	}
	req := wv.OwnRequests[0].ID
	exec(t, app, s.Command{Action: "approve", ID: req})
	wv, e = app.View(ctx, worker, "")
	must(t, e)
	if len(wv.Schedules) != 1 {
		t.Fatal("membership missing")
	}
	if _, e = app.Execute(ctx, admin, s.Command{Action: "team-remove", ParticipationID: part, AccountID: v.AccountID}); !errors.Is(e, s.ErrConflict) {
		t.Fatal("current team readiness not preserved", e)
	}
	_ = edition
	_ = db
}
func TestMigrationFailureRollbackAndRerun(t *testing.T) {
	app, db := setup(t)
	_ = app
	// A separate schema simulates a pre-MVP database with exact legacy values.
	schema := "legacy_" + strings.ToLower(s.ID())
	must(t, db.Exec("CREATE SCHEMA "+schema).Error)
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	must(t, db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL search_path TO " + schema).Error; e != nil {
			return e
		}
		return tx.Exec("CREATE TABLE feedback_notes(id bigserial PRIMARY KEY,title text NOT NULL,created_at timestamptz); INSERT INTO feedback_notes(id,title,created_at) VALUES(42,'Unverändert','2020-01-02T03:04:05Z')").Error
	}))
	run := func(in s.LegacyEdition) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Exec("SET LOCAL search_path TO " + schema).Error; e != nil {
				return e
			}
			return s.Migrate(ctx, tx, in)
		})
	}
	if e := run(s.LegacyEdition{}); e == nil {
		t.Fatal("missing input accepted")
	}
	must(t, db.Transaction(func(tx *gorm.DB) error {
		tx.Exec("SET LOCAL search_path TO " + schema)
		var exists bool
		tx.Raw("SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists)
		if exists {
			return fmt.Errorf("DDL survived rollback")
		}
		return nil
	}))
	must(t, run(s.LegacyEdition{Label: "Explicit legacy", StartDay: "2020-09-10", EndDay: "2020-09-12"}))
	must(t, run(s.LegacyEdition{}))
	if count(t, db, "SELECT count(*) FROM "+schema+".feedback_notes WHERE id=42 AND title='Unverändert' AND created_at='2020-01-02T03:04:05Z' AND edition_id IS NOT NULL") != 1 {
		t.Fatal("backfill changed legacy data")
	}
}
func TestHTTPGuestPrivacyAndOutage(t *testing.T) {
	app, _ := setup(t)
	_, part, _ := baseline(t, app)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	private, public := http.NewServeMux(), http.NewServeMux()
	s.NewHandler(app).Register(private, public, func(h http.Handler) http.Handler { return h })
	r := httptest.NewRequest("GET", "/register/"+token, nil)
	w := httptest.NewRecorder()
	public.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "disabled=\"false\"") || strings.Contains(w.Body.String(), "worker_id") || strings.Contains(w.Body.String(), "id=\"role\"") {
		t.Fatal("guest rendering failed", w.Code)
	}
	app.FGA = nil
	w = httptest.NewRecorder()
	public.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "Aufbau") {
		t.Fatal("outage did not fail closed")
	}
}
func TestInvitationsRevocationAndArchivedOwnRequests(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "global"}).Token
	if kind, e := app.InspectProof(ctx, invite); e != nil || kind != "organizer" {
		t.Fatal(kind, e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, e := app.Execute(ctx, s.Identity{Issuer: "test:issuer", Subject: name}, s.Command{Action: "redeem-organizer", Token: invite})
			results <- e
		}(name)
	}
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !errors.Is(e, s.ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 || count(t, db, "SELECT count(*) FROM global_grants") != 2 {
		t.Fatal("double invite redemption")
	}
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	sub, e := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Own", LastName: "Archived", ShiftIDs: []string{shift}})
	must(t, e)
	who := s.Identity{Issuer: "test:issuer", Subject: "volunteer"}
	_, e = app.Execute(ctx, who, s.Command{Action: "connect", Token: sub.ConnectionToken})
	must(t, e)
	next := exec(t, app, s.Command{Action: "edition-save", Label: "2028", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	exec(t, app, s.Command{Action: "activate", ID: next, Confirm: true})
	if count(t, db, "SELECT count(*) FROM shifts s JOIN participations p ON p.id=s.participation_id WHERE p.edition_id=?", next) != 0 {
		t.Fatal("copy inherited schedule")
	}
	v, e := app.View(ctx, who, "")
	must(t, e)
	if len(v.OwnRequests) != 1 || v.OwnRequests[0].CanWithdraw || len(v.Schedules) != 0 {
		t.Fatal("archive isolation")
	}
	if _, e = app.Execute(ctx, who, s.Command{Action: "withdraw", ID: v.OwnRequests[0].ID}); !errors.Is(e, s.ErrDenied) {
		t.Fatal("archived withdrawal allowed")
	}
	// Operator unlink follows the shared revision lock maintenance procedure.
	must(t, db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT revision FROM scheduling_state WHERE id=1 FOR UPDATE").Error; e != nil {
			return e
		}
		if e := tx.Exec("UPDATE workers SET account_id=NULL WHERE id=?", sub.WorkerID).Error; e != nil {
			return e
		}
		return tx.Exec("UPDATE scheduling_state SET revision=revision+1 WHERE id=1").Error
	}))
	if _, e = app.Worker(ctx, who, sub.WorkerID); !errors.Is(e, s.ErrDenied) {
		t.Fatal("unlink stale access")
	}
	old, e := app.View(ctx, admin, edition)
	must(t, e)
	account := old.AccountID
	exec(t, app, s.Command{Action: "team-remove", ParticipationID: part, AccountID: account})
	if _, e = app.Execute(ctx, admin, s.Command{Action: "shift-save", ID: shift, Title: "Forbidden", Start: "2027-09-10T09:00:00+02:00", End: "2027-09-10T12:00:00+02:00", Capacity: 1, Version: 1}); !errors.Is(e, s.ErrDenied) {
		t.Fatal("global implicit schedule edit")
	}
}
func TestDateCorrectionsAndCapacityReduction(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	var stand string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	worker := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Manual", LastName: "Worker"}).ID
	exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: worker})
	exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: worker})
	if count(t, db, "SELECT count(*) FROM assignments") != 1 {
		t.Fatal("duplicate assignment")
	}
	if _, e := app.Execute(ctx, admin, s.Command{Action: "member-remove", StandID: stand, WorkerID: worker}); !errors.Is(e, s.ErrConflict) {
		t.Fatal("membership removal with future assignment")
	}
	if _, e := app.Execute(ctx, admin, s.Command{Action: "shift-save", ID: shift, Title: "Changed", Start: "2027-09-10T10:00:00+02:00", End: "2027-09-10T13:00:00+02:00", Capacity: 1, Version: 1}); !errors.Is(e, s.ErrConflict) {
		t.Fatal("time edit without confirmation")
	}
	exec(t, app, s.Command{Action: "edition-save", ID: edition, Label: "Renamed", StartDay: "2027-12-10", EndDay: "2027-12-12"})
	v, e := app.View(ctx, admin, "")
	must(t, e)
	if !v.Schedules[0].Shifts[0].Outside {
		t.Fatal("date correction missing flag")
	}
	exec(t, app, s.Command{Action: "shift-cancel", ID: shift, Confirm: true})
	if count(t, db, "SELECT count(*) FROM assignments WHERE active") != 0 || count(t, db, "SELECT count(*) FROM memberships") != 1 {
		t.Fatal("cancel removed membership/history")
	}
}

type blockingChecker struct {
	base             s.Checker
	started, release chan struct{}
	once             sync.Once
}

func (b *blockingChecker) CheckContext(c context.Context, u, r, o string, f []s.Tuple) (bool, error) {
	if r == "edit_schedule" {
		b.once.Do(func() { close(b.started); <-b.release })
	}
	return b.base.CheckContext(c, u, r, o, f)
}
func TestAuthorizationSnapshotSerializesWithRevocation(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	next := exec(t, app, s.Command{Action: "edition-save", Label: "2028", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	exec(t, app, s.Command{Action: "activate", ID: next, Confirm: true})
	v, e := app.View(ctx, admin, edition)
	must(t, e)
	worker := s.ID()
	must(t, db.Exec("INSERT INTO workers(id,first_name,last_name) VALUES(?,'Race','Worker')", worker).Error)
	blocker := &blockingChecker{base: app.FGA, started: make(chan struct{}), release: make(chan struct{})}
	app.FGA = blocker
	first := make(chan error, 1)
	go func() {
		_, e := app.Execute(ctx, admin, s.Command{Action: "assign", ID: shift, WorkerID: worker})
		first <- e
	}()
	<-blocker.started
	revoked := make(chan error, 1)
	go func() {
		_, e := app.Execute(ctx, admin, s.Command{Action: "team-remove", ParticipationID: part, AccountID: v.AccountID})
		revoked <- e
	}()
	select {
	case e := <-revoked:
		t.Fatal("revocation passed uncommitted mutation", e)
	case <-time.After(40 * time.Millisecond):
	}
	close(blocker.release)
	must(t, <-first)
	must(t, <-revoked)
	_, e = app.Execute(ctx, admin, s.Command{Action: "unassign", ID: shift, WorkerID: worker})
	if !errors.Is(e, s.ErrDenied) {
		t.Fatal("next request used stale team", e)
	}
}
func TestBerlinWindowAndClockChanges(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2027-09-10")
	end, _ := time.Parse("2006-01-02", "2027-09-12")
	edition := s.Edition{StartDay: start, EndDay: end}
	a, _ := time.Parse(time.RFC3339, "2027-08-27T00:00:00+02:00")
	b, _ := time.Parse(time.RFC3339, "2027-09-27T00:00:00+02:00")
	if !(s.Shift{StartsAt: a, EndsAt: b}).Within(edition) {
		t.Fatal("inclusive calendar window")
	}
	if (s.Shift{StartsAt: a.Add(-time.Second), EndsAt: b}).Within(edition) {
		t.Fatal("outside window accepted")
	}
	before, _ := time.Parse(time.RFC3339, "2027-10-31T02:30:00+02:00")
	after, _ := time.Parse(time.RFC3339, "2027-10-31T02:30:00+01:00")
	if !after.After(before) || !(s.Shift{StartsAt: before, EndsAt: after}).EndsAt.After(before) {
		t.Fatal("clock change ambiguous")
	}
}
func TestWorkerReuseMaskingAndFeedbackIsolation(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	submit := func(first string) s.Submission {
		v, e := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: first, LastName: "Duplicate", ShiftIDs: []string{shift}})
		must(t, e)
		return v
	}
	one := submit("First")
	who := s.Identity{Issuer: "test:issuer", Subject: "connected"}
	_, e := app.Execute(ctx, who, s.Command{Action: "connect", Token: one.ConnectionToken})
	must(t, e)
	two := submit("Second")
	_, e = app.Execute(ctx, who, s.Command{Action: "connect", Token: two.ConnectionToken})
	must(t, e)
	if count(t, db, "SELECT count(*) FROM workers WHERE id IN (?,?)", one.WorkerID, two.WorkerID) != 1 || count(t, db, "SELECT count(*) FROM requests") != 1 {
		t.Fatal("fresh reuse duplicated worker/request")
	}
	three := submit("Third")
	link := exec(t, app, s.Command{Action: "connection-link", WorkerID: three.WorkerID}).Token
	if _, e = app.Execute(ctx, who, s.Command{Action: "connect", Token: link}); !errors.Is(e, s.ErrConflict) {
		t.Fatal("historical connection stole worker")
	}
	replacement := exec(t, app, s.Command{Action: "connection-link", WorkerID: three.WorkerID}).Token
	if _, e = app.InspectProof(ctx, link); !errors.Is(e, s.ErrDenied) {
		t.Fatal("replaced link still usable")
	}
	app.Now = func() time.Time { return time.Date(2027, 9, 9, 0, 0, 0, 0, s.Berlin) }
	if _, e = app.InspectProof(ctx, replacement); !errors.Is(e, s.ErrDenied) {
		t.Fatal("expired connection usable")
	}
	must(t, app.AddFeedback(ctx, admin, edition, "Edition-private"))
	next := exec(t, app, s.Command{Action: "edition-save", Label: "Next", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	view, e := app.View(ctx, admin, next)
	must(t, e)
	if len(view.Notes) != 0 {
		t.Fatal("cross-edition feedback")
	}
	if e = app.AddFeedback(ctx, who, edition, "forbidden"); !errors.Is(e, s.ErrDenied) {
		t.Fatal("non-global feedback")
	}
}
