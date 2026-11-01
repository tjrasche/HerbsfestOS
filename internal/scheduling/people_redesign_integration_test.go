package scheduling_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
)

func TestRedesignPeoplePagedSearchPreservesMasking(t *testing.T) {
	app, db := setup(t)
	_, part, _ := baseline(t, app)
	invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part}).Token
	organiser := s.Identity{Issuer: "test:issuer", Subject: "scoped-directory-editor"}
	_, err := app.Execute(ctx, organiser, s.Command{Action: "redeem-organizer", Token: invite})
	must(t, err)
	for i := 0; i < 57; i++ {
		must(t, db.Exec("INSERT INTO workers(id,first_name,last_name,email,phone,notes) VALUES(?,?,?,?,?,?)", fmt.Sprintf("search-%03d", i), "Match", fmt.Sprintf("Name%03d", i), fmt.Sprintf("private-%03d@example.invalid", i), fmt.Sprintf("09000%03d", i), "DO NOT DISCLOSE").Error)
	}
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		v, err := app.SearchWorkers(ctx, organiser, s.DirectoryQuery{Query: "Match", Page: page})
		must(t, err)
		want := 25
		if page == 3 {
			want = 7
		}
		if len(v.Rows) != want || v.HasNext != (page < 3) {
			t.Fatalf("page %d: %d rows next=%v", page, len(v.Rows), v.HasNext)
		}
		for _, row := range v.Rows {
			if seen[row.ID] || row.Full || strings.Contains(row.Email, "@example") || row.Notes != "" || row.AccountID != nil {
				t.Fatalf("bad or repeated masked row: %#v", row)
			}
			seen[row.ID] = true
		}
		var b bytes.Buffer
		must(t, s.DirectoryContent(v, s.FormState{}).Render(ctx, &b))
		if strings.Contains(b.String(), "DO NOT DISCLOSE") || strings.Contains(b.String(), "private-000@example.invalid") {
			t.Fatal("private directory data reached HTML")
		}
	}
	v, err := app.SearchWorkers(ctx, organiser, s.DirectoryQuery{Query: "%%", Page: 1})
	must(t, err)
	if len(v.Rows) != 0 {
		t.Fatal("SQL wildcard treated as name wildcard")
	}
	v, err = app.SearchWorkers(ctx, organiser, s.DirectoryQuery{Query: "private-056@example.invalid", Page: 1})
	must(t, err)
	if len(v.Rows) != 1 || v.Rows[0].ID != "search-056" {
		t.Fatal("exact contact lookup changed")
	}
	if _, err = app.SearchWorkers(ctx, s.Identity{Issuer: "test:issuer", Subject: "volunteer"}, s.DirectoryQuery{Query: "Match"}); !errors.Is(err, s.ErrDenied) {
		t.Fatal("volunteer directory was not denied", err)
	}
}

func TestRedesignOwnContactOmissionPreservesSharedNotes(t *testing.T) {
	app, db := setup(t)
	_, part, _ := baseline(t, app)
	var stand string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	worker := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Ada", LastName: "Helferin"}).ID
	must(t, db.Exec("UPDATE workers SET notes='Gemeinsam sichtbar' WHERE id=?", worker).Error)
	proof := exec(t, app, s.Command{Action: "connection-link", WorkerID: worker}).Token
	who := s.Identity{Issuer: "test:issuer", Subject: "linked-worker"}
	_, err := app.Execute(ctx, who, s.Command{Action: "connect", Token: proof})
	must(t, err)
	_, err = app.Execute(ctx, who, s.Command{Action: "worker-save", ID: worker, FirstName: "Ada", LastName: "Helferin", Email: "ada@example.invalid", PreserveNotes: true})
	must(t, err)
	w, err := app.Worker(ctx, who, worker)
	must(t, err)
	if w.Notes != "Gemeinsam sichtbar" || w.Email != "ada@example.invalid" {
		t.Fatal("contact save lost notes", w)
	}
	_, err = app.Execute(ctx, who, s.Command{Action: "worker-save", ID: worker, FirstName: "Ada", LastName: "Helferin", Notes: "forged note"})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal("owner edited shared notes", err)
	}
	v, err := app.WorkerDetail(ctx, who, "", worker)
	must(t, err)
	if v.CanEditNotes || !v.CanEditContacts {
		t.Fatal("profile action booleans wrong")
	}
}

func TestRedesignMergeRequiresFreshPreviewAndExplicitDataChoices(t *testing.T) {
	app, db := setup(t)
	_, part, _ := baseline(t, app)
	var stand string
	must(t, db.Raw("SELECT stand_id FROM participations WHERE id=?", part).Scan(&stand).Error)
	a := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Ada", LastName: "Gleich"}).ID
	b := exec(t, app, s.Command{Action: "member-add", StandID: stand, FirstName: "Ada", LastName: "Gleich"}).ID
	must(t, db.Exec("UPDATE workers SET email='first@example.invalid',notes='Notiz A' WHERE id=?", a).Error)
	must(t, db.Exec("UPDATE workers SET email='second@example.invalid',notes='Notiz B' WHERE id=?", b).Error)
	p, err := app.MergePreview(ctx, admin, a, b)
	must(t, err)
	if !p.EmailConflict || p.Result.Notes != "Notiz A\n\nNotiz B" {
		t.Fatal("merge defaults dropped context")
	}
	c := s.Command{Action: "worker-merge", ID: a, OtherWorkerID: b, FirstName: "Ada", LastName: "Gleich", Confirm: true}
	if _, err = app.Execute(ctx, admin, c); !errors.Is(err, s.ErrConflict) {
		t.Fatal("merge accepted missing preview", err)
	}
	c.PreviewFingerprint = p.Fingerprint
	if _, err = app.Execute(ctx, admin, c); !errors.Is(err, s.ErrInvalid) {
		t.Fatal("merge erased contacts/notes without clear", err)
	}
	c.EmailChoice = "target"
	c.Notes = p.Result.Notes
	must(t, db.Exec("UPDATE workers SET phone='changed' WHERE id=?", b).Error)
	if _, err = app.Execute(ctx, admin, c); !errors.Is(err, s.ErrConflict) {
		t.Fatal("stale preview accepted", err)
	}
	p, err = app.MergePreview(ctx, admin, a, b)
	must(t, err)
	c.PreviewFingerprint = p.Fingerprint
	c.Phone = p.Result.Phone
	_, err = app.Execute(ctx, admin, c)
	must(t, err)
	w, err := app.Worker(ctx, admin, a)
	must(t, err)
	if w.Email != "first@example.invalid" || w.Phone != "changed" || w.Notes != "Notiz A\n\nNotiz B" {
		t.Fatal("merge retained wrong result", w)
	}
	if count(t, db, "SELECT count(*) FROM workers WHERE id=?", b) != 0 || count(t, db, "SELECT count(*) FROM scheduling_audit WHERE action='worker-merge' AND resource=?", a) != 1 {
		t.Fatal("merge audit/deletion changed")
	}
}

func TestRedesignCapabilitiesUseNonBearerManagementAndScopedReceipt(t *testing.T) {
	app, _ := setup(t)
	_, part, shift := baseline(t, app)
	issued := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part})
	info, err := app.ProofContext(ctx, issued.Token, s.Identity{})
	must(t, err)
	if info.Scope != "team" || info.Stand != "Bierstand" || info.CanProceed || info.SignedIn {
		t.Fatal("proof scope/auth context wrong", info)
	}
	info, err = app.ProofContext(ctx, issued.Token, admin)
	must(t, err)
	if !info.CanProceed || !info.SignedIn {
		t.Fatal("signed-in proof unavailable")
	}
	replacement := exec(t, app, s.Command{Action: "capability-reissue", CapabilityID: issued.ID})
	if _, err = app.InspectProof(ctx, issued.Token); !errors.Is(err, s.ErrDenied) {
		t.Fatal("old proof survived replacement", err)
	}
	if _, err = app.InspectProof(ctx, replacement.ID); !errors.Is(err, s.ErrDenied) {
		t.Fatal("management record became bearer credential", err)
	}
	if _, err = app.ProofContext(ctx, replacement.Token, admin); err != nil {
		t.Fatal("new proof invalid", err)
	}
	exec(t, app, s.Command{Action: "capability-revoke", CapabilityID: replacement.ID})
	v, err := app.CapabilityDetail(ctx, admin, replacement.ID, "")
	must(t, err)
	if v.Link.Status != "Widerrufen" {
		t.Fatal("status wrong", v.Link.Status)
	}
	signup := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part})
	in := s.SignupInput{Token: signup.Token, Key: s.ID() + s.ID(), FirstName: "Bea", LastName: "Gast", ShiftIDs: []string{shift}}
	one, err := app.Submit(ctx, s.Identity{}, in)
	must(t, err)
	if len(one.Receipt.Items) != 1 || one.Receipt.Items[0].Stand != "Bierstand" || one.Receipt.Items[0].Status != "pending" {
		t.Fatal("receipt lacks canonical selection", one.Receipt)
	}
	app.Now = func() time.Time { return time.Date(2028, 1, 1, 0, 0, 0, 0, s.Berlin) }
	again, err := app.Submit(ctx, s.Identity{}, in)
	must(t, err)
	if !reflect.DeepEqual(one, again) {
		t.Fatal("retry after expiry changed scoped receipt")
	}
}
