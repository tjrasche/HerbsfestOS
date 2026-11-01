package scheduling_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

func ownRequestWorker(t *testing.T, app *s.Store, db *gorm.DB, part, shift string, who s.Identity) (string, string) {
	t.Helper()
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	sub, err := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Eigene", LastName: "Anfrage", ShiftIDs: []string{shift}})
	must(t, err)
	_, err = app.Execute(ctx, who, s.Command{Action: "connect", Token: sub.ConnectionToken})
	must(t, err)
	var request string
	must(t, db.Raw("SELECT id FROM requests WHERE worker_id=? AND shift_id=?", sub.WorkerID, shift).Scan(&request).Error)
	return sub.WorkerID, request
}

func ownRequest(t *testing.T, app *s.Store, who s.Identity, filter string) s.RequestRow {
	t.Helper()
	page, err := app.OwnRequests(ctx, who, "", filter, 1)
	must(t, err)
	if len(page.Rows) != 1 {
		t.Fatalf("expected one own request, got %+v", page.Rows)
	}
	return page.Rows[0]
}

func TestOwnRequestsReflectCancellationAndAssignmentRemoval(t *testing.T) {
	for _, end := range []string{"cancel", "unassign"} {
		t.Run(end, func(t *testing.T) {
			app, db := setup(t)
			_, part, shift := baseline(t, app)
			who := s.Identity{Issuer: "test:issuer", Subject: "own-request-" + end}
			worker, request := ownRequestWorker(t, app, db, part, shift, who)
			exec(t, app, s.Command{Action: "approve", ID: request})
			row := ownRequest(t, app, who, "approved")
			if row.Status != "approved" || row.DisplayStatus != "approved" || !row.Assigned || row.ShiftCancelled || row.Stale || row.CanWithdraw {
				t.Fatalf("active approval is incorrect: %+v", row)
			}
			exec(t, app, s.Command{Action: "shift-save", ID: shift, Title: "Geänderte Zeiten", Start: "2027-09-10T10:00:00+02:00", End: "2027-09-10T13:00:00+02:00", Capacity: 1, Version: 1, Confirm: true})
			if row = ownRequest(t, app, who, "approved"); !row.Stale {
				t.Fatal("a changed active duty lost its time-change warning")
			}
			want := "removed"
			if end == "cancel" {
				exec(t, app, s.Command{Action: "shift-cancel", ID: shift, Confirm: true})
				want = "cancelled"
			} else {
				exec(t, app, s.Command{Action: "unassign", ID: shift, WorkerID: worker})
			}
			row = ownRequest(t, app, who, want)
			if row.ID != request || row.Status != "approved" || row.DisplayStatus != want || row.Assigned || row.ShiftCancelled != (end == "cancel") || row.Stale || row.CanWithdraw {
				t.Fatalf("ended duty still appears confirmed or actionable: %+v", row)
			}
			approved, err := app.OwnRequests(ctx, who, "", "approved", 1)
			must(t, err)
			if len(approved.Rows) != 0 || approved.HasNext {
				t.Fatal("ended duty remained in the confirmed filter")
			}
			if count(t, db, "SELECT count(*) FROM requests WHERE id=? AND status='approved'", request) != 1 {
				t.Fatal("reading current duty state rewrote approval history")
			}
			if end == "unassign" {
				// Legacy data may have no assignment row, rather than an inactive one.
				must(t, db.Exec("DELETE FROM assignments WHERE shift_id=? AND worker_id=?", shift, worker).Error)
				if row = ownRequest(t, app, who, "removed"); row.Assigned || row.Stale {
					t.Fatalf("missing assignment was treated as confirmed: %+v", row)
				}
				exec(t, app, s.Command{Action: "assign", ID: shift, WorkerID: worker})
				if row = ownRequest(t, app, who, "approved"); !row.Assigned || !row.Stale {
					t.Fatalf("reassigned duty did not reflect fresh assignment state: %+v", row)
				}
			}
		})
	}
}

func TestOwnRequestsDerivedFiltersAndPaginationStayConsistent(t *testing.T) {
	app, db := setup(t)
	_, part, shift := baseline(t, app)
	who := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
	worker, _ := ownRequestWorker(t, app, db, part, shift, who)
	// Trusted synthetic historical fixtures exercise filtering before LIMIT and
	// the fallback count, including rows predating this read-model correction.
	for _, kind := range []string{"cancelled", "removed", "approved"} {
		for i := 0; i < 21; i++ {
			id := fmt.Sprintf("own-%s-%02d", kind, i)
			must(t, db.Exec("INSERT INTO shifts(id,participation_id,title,starts_at,ends_at,capacity,cancelled,version) VALUES(?,?,?,'2027-09-10 09:00+02','2027-09-10 12:00+02',3,?,2)", id, part, id, kind == "cancelled").Error)
			must(t, db.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version) VALUES(?,?,?,'approved',1)", id, id, worker).Error)
			if kind != "removed" {
				// Cancelled legacy duties can even retain an active assignment.
				must(t, db.Exec("INSERT INTO assignments(shift_id,worker_id) VALUES(?,?)", id, worker).Error)
			}
		}
	}
	other := s.Identity{Issuer: "test:issuer", Subject: "other-request-owner"}
	otherWorker, otherRequest := ownRequestWorker(t, app, db, part, shift, other)
	// Another worker's assignment must not confirm the first worker's request.
	must(t, db.Exec("INSERT INTO assignments(shift_id,worker_id) VALUES('own-removed-00',?)", otherWorker).Error)
	web := uiHTTP(t, app, func() {})
	for _, kind := range []string{"cancelled", "removed", "approved"} {
		seen := map[string]bool{}
		for page := 1; page <= 2; page++ {
			out, err := app.OwnRequests(ctx, who, "", kind, page)
			must(t, err)
			wantRows := 20
			if page == 2 {
				wantRows = 1
			}
			if out.Filter.Page != page || len(out.Rows) != wantRows || out.HasNext != (page == 1) || (out.Previous != "") != (page == 2) {
				t.Fatalf("%s page %d has wrong page metadata: %+v", kind, page, out)
			}
			link := out.Next
			if page == 2 {
				link = out.Previous
			}
			u, err := url.Parse(link)
			must(t, err)
			if u.Query().Get("status") != kind {
				t.Fatalf("%s paging lost its effective status: %s", kind, link)
			}
			for _, row := range out.Rows {
				if row.Status != "approved" || row.DisplayStatus != kind || row.WorkerID != worker || seen[row.ID] || (kind != "approved" && (row.Stale || row.CanWithdraw)) {
					t.Fatalf("filtered page contains an incorrect or repeated row: %+v", row)
				}
				seen[row.ID] = true
			}
		}
		out, err := app.OwnRequests(ctx, who, "", kind, 99)
		must(t, err)
		if out.Filter.Page != 2 || len(out.Rows) != 1 || out.HasNext {
			t.Fatalf("%s out-of-range fallback counted a different status: %+v", kind, out)
		}
		for _, hx := range []bool{false, true} {
			for _, page := range []int{2, 99} {
				r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/requests/mine?status=%s&page=%d", kind, page), nil)
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				web.ServeHTTP(w, r)
				body := w.Body.String()
				if w.Code != http.StatusOK || strings.Count(body, `<li class="record-row">`) != 1 || !strings.Contains(body, `page=1&amp;status=`+kind) {
					t.Fatalf("HTTP paging disagreed with effective status page: %d %s", w.Code, body)
				}
			}
		}
	}
	last, err := app.OwnRequests(ctx, who, "", "approved", 2)
	must(t, err)
	exec(t, app, s.Command{Action: "unassign", ID: last.Rows[0].ShiftID, WorkerID: worker})
	last, err = app.OwnRequests(ctx, who, "", "approved", 2)
	must(t, err)
	if last.Filter.Page != 1 || len(last.Rows) != 20 || last.HasNext {
		t.Fatalf("last removed confirmation did not recover to the remaining page: %+v", last)
	}
	all, err := app.OwnRequests(ctx, who, "", "", 99)
	must(t, err)
	if all.Filter.Page != 4 || len(all.Rows) != 4 || all.HasNext {
		t.Fatalf("unfiltered requests lost rows during status derivation: %+v", all)
	}
	if row := ownRequest(t, app, other, "pending"); row.ID != otherRequest || row.WorkerID != otherWorker {
		t.Fatalf("other actor saw another worker's requests: %+v", row)
	}
	if _, err = app.OwnRequests(ctx, who, "", "invalid-status", 1); !errors.Is(err, s.ErrInvalid) {
		t.Fatal("invalid effective filter was accepted", err)
	}
}

func TestOwnRequestsFullAndFrameShowEndedDutyContext(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	who := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
	worker, request := ownRequestWorker(t, app, db, part, shift, who)
	exec(t, app, s.Command{Action: "approve", ID: request})
	exec(t, app, s.Command{Action: "shift-cancel", ID: shift, Confirm: true})
	for _, kind := range []string{"legacy-cancelled", "removed"} {
		must(t, db.Exec("INSERT INTO shifts(id,participation_id,title,starts_at,ends_at,capacity,cancelled,version) VALUES(?,?,?,'2027-09-10 09:00+02','2027-09-10 12:00+02',1,?,2)", kind, part, kind, kind == "legacy-cancelled").Error)
		must(t, db.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version) VALUES(?,?,?,'approved',1)", kind, kind, worker).Error)
	}
	// Another actor's private request must stay absent from every response mode.
	privateShift := exec(t, app, s.Command{Action: "shift-save", ParticipationID: part, Title: "PRIVATE OTHER DUTY", Start: "2027-09-10T09:00:00+02:00", End: "2027-09-10T12:00:00+02:00", Capacity: 1}).ID
	ownRequestWorker(t, app, db, part, privateShift, s.Identity{Issuer: "test:issuer", Subject: "private-owner"})
	web := uiHTTP(t, app, func() {})
	next := exec(t, app, s.Command{Action: "edition-save", Label: "2028", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	for _, archived := range []bool{false, true} {
		if archived {
			exec(t, app, s.Command{Action: "activate", ID: next, Confirm: true})
		}
		for _, mode := range []string{"full", "frame", "history"} {
			for _, filter := range []string{"", "cancelled", "removed", "approved"} {
				t.Run(fmt.Sprintf("archived=%t/%s/%s", archived, mode, filter), func(t *testing.T) {
					path := "/requests/mine"
					if filter != "" {
						path += "?status=" + filter
					}
					r := httptest.NewRequest(http.MethodGet, path, nil)
					if mode != "full" {
						r.Header.Set("HX-Request", "true")
					}
					if mode == "history" {
						r.Header.Set("HX-History-Restore-Request", "true")
					}
					w := httptest.NewRecorder()
					web.ServeHTTP(w, r)
					body := w.Body.String()
					if w.Code != http.StatusOK || strings.Count(body, `id="app-frame"`) != 1 || strings.Contains(strings.ToLower(body), "<!doctype html>") != (mode != "frame") {
						t.Fatalf("invalid own-request response: %d %s", w.Code, body)
					}
					if strings.Contains(body, "PRIVATE OTHER DUTY") || strings.Contains(body, "Dienstzeiten wurden geändert") || strings.Contains(body, "</span>Bestätigt</span>") || strings.Contains(body, "/withdraw") {
						t.Fatalf("response leaked another actor, confirmation, warning, or action: %s", body)
					}
					wantCancelled := filter == "" || filter == "cancelled"
					wantRemoved := filter == "" || filter == "removed"
					if strings.Contains(body, "Dieser Dienst wurde abgesagt.") != wantCancelled || strings.Contains(body, "legacy-cancelled</h2>") != wantCancelled || strings.Contains(body, "Deine Einteilung für diesen Dienst wurde aufgehoben.") != wantRemoved {
						t.Fatalf("effective filter or ended-duty context is incorrect: %s", body)
					}
					if wantCancelled && strings.Count(body, "</span>Abgesagt</span>") != 2 {
						t.Fatal("command and legacy cancellations did not both render cancelled badges")
					}
					if wantRemoved && !strings.Contains(body, "</span>Einteilung aufgehoben</span>") {
						t.Fatal("removed assignment badge is missing")
					}
					if strings.Contains(body, "· Archiv") != (archived && filter != "approved") {
						t.Fatal("ended duties lost their edition archive context")
					}
				})
			}
		}
	}
}

func TestOwnRequestsCurrentAndArchivedActionsRemainScoped(t *testing.T) {
	app, db := setup(t)
	edition, part, shift := baseline(t, app)
	who := s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"}
	_, request := ownRequestWorker(t, app, db, part, shift, who)
	web := uiHTTP(t, app, func() {})
	if row := ownRequest(t, app, who, "pending"); !row.CanWithdraw {
		t.Fatal("a current pending request lost its withdrawal action")
	}
	next := exec(t, app, s.Command{Action: "edition-save", Label: "2028", StartDay: "2028-09-10", EndDay: "2028-09-12", SourceID: edition}).ID
	for _, archived := range []bool{false, true} {
		if archived {
			exec(t, app, s.Command{Action: "activate", ID: next, Confirm: true})
		}
		row := ownRequest(t, app, who, "pending")
		if row.DisplayStatus != "pending" || row.CanWithdraw == archived {
			t.Fatalf("current/archive withdrawal state is incorrect: %+v", row)
		}
		for _, hx := range []bool{false, true} {
			r := httptest.NewRequest(http.MethodGet, "/requests/mine?status=pending", nil)
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			web.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != http.StatusOK || strings.Contains(body, "/requests/"+request+"/withdraw") == archived || strings.Contains(body, "· Archiv") != archived {
				t.Fatalf("current/archive form rendering is incorrect: %d %s", w.Code, body)
			}
			if archived {
				values := url.Values{"return_to": {"/requests/mine?status=pending"}}
				r = httptest.NewRequest(http.MethodPost, "/requests/"+request+"/withdraw", strings.NewReader(values.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w = httptest.NewRecorder()
				web.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || count(t, db, "SELECT count(*) FROM requests WHERE id=? AND status='pending'", request) != 1 {
					t.Fatalf("archived withdrawal changed history: %d", w.Code)
				}
			}
		}
	}
}
