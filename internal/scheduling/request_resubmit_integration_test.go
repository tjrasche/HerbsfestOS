package scheduling_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

type approvedSignup struct {
	app                 *s.Store
	db                  *gorm.DB
	part, shift, worker string
	request             s.Request
	original            s.SignupInput
	submission          s.Submission
	who                 s.Identity
}

func setupApprovedSignup(t *testing.T, who s.Identity) approvedSignup {
	t.Helper()
	app, db := setup(t)
	_, part, shift := baseline(t, app)
	token := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part}).Token
	in := s.SignupInput{Token: token, Key: s.ID() + s.ID(), FirstName: "Anna", LastName: "Wiederkehr", ShiftIDs: []string{shift}}
	submitted, err := app.Submit(ctx, s.Identity{}, in)
	must(t, err)
	_, err = app.Execute(ctx, who, s.Command{Action: "connect", Token: submitted.ConnectionToken})
	must(t, err)
	var request s.Request
	must(t, db.Raw("SELECT * FROM requests WHERE worker_id=?", submitted.WorkerID).Scan(&request).Error)
	exec(t, app, s.Command{Action: "approve", ID: request.ID})
	request.Status = "approved"
	return approvedSignup{app: app, db: db, part: part, shift: shift, worker: submitted.WorkerID, request: request, original: in, submission: submitted, who: who}
}

func (f approvedSignup) memberInput() s.SignupInput {
	in := f.original
	in.Token = ""
	in.Key = s.ID() + s.ID()
	return in
}

func (f approvedSignup) currentRequest(t *testing.T) s.Request {
	t.Helper()
	var request s.Request
	must(t, f.db.Raw("SELECT * FROM requests WHERE id=?", f.request.ID).Scan(&request).Error)
	return request
}

func TestApprovedSignupResubmitsAfterUnassignment(t *testing.T) {
	f := setupApprovedSignup(t, s.Identity{Issuer: "test:issuer", Subject: "resubmitting-worker"})
	// Spare capacity allows a new key to reach the already-approved request.
	exec(t, f.app, s.Command{Action: "shift-save", ID: f.shift, Title: "Aufbau", Start: "2027-09-10T09:00:00+02:00", End: "2027-09-10T12:00:00+02:00", Capacity: 2, Version: 1})
	active, err := f.app.Submit(ctx, f.who, f.memberInput())
	must(t, err)
	if len(active.Receipt.Items) != 1 || active.Receipt.Items[0].Status != "approved" || !reflect.DeepEqual(f.currentRequest(t), f.request) {
		t.Fatal("new key changed an actively assigned approval", active)
	}
	exec(t, f.app, s.Command{Action: "unassign", ID: f.shift, WorkerID: f.worker})
	exec(t, f.app, s.Command{Action: "shift-save", ID: f.shift, Title: "Aufbau später", Start: "2027-09-10T10:00:00+02:00", End: "2027-09-10T13:00:00+02:00", Capacity: 2, Version: 1})
	// An old submission key remains a receipt retry, not a new request.
	retried, err := f.app.Submit(ctx, s.Identity{}, f.original)
	must(t, err)
	if retried.WorkerID != f.worker || retried.ConnectionToken != f.submission.ConnectionToken || retried.Receipt.Items[0].Status != "approved" || !reflect.DeepEqual(f.currentRequest(t), f.request) {
		t.Fatal("original guest retry reopened an unassigned approval", retried)
	}
	now := f.app.Now().Add(time.Hour)
	f.app.Now = func() time.Time { return now }
	in := f.memberInput()
	resubmitted, err := f.app.Submit(ctx, f.who, in)
	must(t, err)
	wantStart, err := time.Parse(time.RFC3339, "2027-09-10T10:00:00+02:00")
	must(t, err)
	if resubmitted.WorkerID != f.worker || resubmitted.ConnectionToken != "" || len(resubmitted.Receipt.Items) != 1 || resubmitted.Receipt.Items[0].Status != "pending" || !resubmitted.Receipt.Items[0].StartsAt.Equal(wantStart) {
		t.Fatal("resubmission did not return a pending receipt for the current shift", resubmitted)
	}
	request := f.currentRequest(t)
	if request.ID != f.request.ID || request.Status != "pending" || request.ShiftVersion != 2 || !request.CreatedAt.Equal(now) {
		t.Fatal("resubmission did not refresh the existing request", request)
	}
	queue, err := f.app.PendingRequests(ctx, admin, s.ScheduleFilter{Stand: f.part})
	must(t, err)
	if len(queue.Rows) != 1 || queue.Rows[0].ID != request.ID || queue.Rows[0].Stale || !queue.Rows[0].CanEdit {
		t.Fatal("resubmission missing from the fresh approval queue", queue.Rows)
	}
	// Both retry forms preserve a pending request, including its original time version.
	exec(t, f.app, s.Command{Action: "shift-save", ID: f.shift, Title: "Aufbau noch später", Start: "2027-09-10T11:00:00+02:00", End: "2027-09-10T14:00:00+02:00", Capacity: 2, Version: 2})
	for _, input := range []s.SignupInput{in, f.memberInput()} {
		out, err := f.app.Submit(ctx, f.who, input)
		must(t, err)
		if out.Receipt.Items[0].Status != "pending" || !reflect.DeepEqual(f.currentRequest(t), request) {
			t.Fatal("pending retry changed the request", out)
		}
	}
	in.LastName = "Different"
	if _, err := f.app.Submit(ctx, f.who, in); !errors.Is(err, s.ErrConflict) {
		t.Fatal("changed payload reused a committed key", err)
	}
	if count(t, f.db, "SELECT count(*) FROM requests") != 1 || count(t, f.db, "SELECT count(*) FROM assignments") != 1 || count(t, f.db, "SELECT count(*) FROM assignments WHERE active") != 0 || count(t, f.db, "SELECT count(*) FROM memberships") != 1 || count(t, f.db, "SELECT count(*) FROM scheduling_audit WHERE action='request-resubmitted' AND resource=?", request.ID) != 1 {
		t.Fatal("resubmission duplicated history or restored an assignment without approval")
	}
	exec(t, f.app, s.Command{Action: "approve", ID: request.ID})
	if count(t, f.db, "SELECT count(*) FROM assignments") != 1 || count(t, f.db, "SELECT count(*) FROM assignments WHERE active") != 1 || count(t, f.db, "SELECT count(*) FROM memberships") != 1 {
		t.Fatal("fresh approval did not reuse the assignment and membership")
	}
}

func TestApprovedSignupResubmissionValidatesWholeBatch(t *testing.T) {
	f := setupApprovedSignup(t, s.Identity{Issuer: "test:issuer", Subject: "atomic-resubmitting-worker"})
	exec(t, f.app, s.Command{Action: "unassign", ID: f.shift, WorkerID: f.worker})
	other := exec(t, f.app, s.Command{Action: "shift-save", ParticipationID: f.part, Title: "Weiterer Dienst", Start: "2027-09-10T13:00:00+02:00", End: "2027-09-10T15:00:00+02:00", Capacity: 1}).ID
	stand := ""
	must(t, f.db.Raw("SELECT stand_id FROM participations WHERE id=?", f.part).Scan(&stand).Error)
	occupant := exec(t, f.app, s.Command{Action: "member-add", StandID: stand, FirstName: "Besetzter", LastName: "Platz"}).ID
	exec(t, f.app, s.Command{Action: "assign", ID: other, WorkerID: occupant})
	cancelled := exec(t, f.app, s.Command{Action: "shift-save", ParticipationID: f.part, Title: "Abgesagt", Start: "2027-09-10T15:00:00+02:00", End: "2027-09-10T17:00:00+02:00", Capacity: 1}).ID
	exec(t, f.app, s.Command{Action: "shift-cancel", ID: cancelled, Confirm: true})
	for _, tc := range []struct {
		name   string
		shifts []string
		want   error
	}{
		{"full", []string{f.shift, other}, s.ErrConflict},
		{"cancelled", []string{f.shift, cancelled}, s.ErrConflict},
		{"missing", []string{f.shift, "missing"}, s.ErrInvalid},
		{"duplicate", []string{f.shift, f.shift}, s.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := f.memberInput()
			in.ShiftIDs = tc.shifts
			if _, err := f.app.Submit(ctx, f.who, in); !errors.Is(err, tc.want) {
				t.Fatalf("invalid batch returned %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(f.currentRequest(t), f.request) || count(t, f.db, "SELECT count(*) FROM requests") != 1 || count(t, f.db, "SELECT count(*) FROM workers") != 2 || count(t, f.db, "SELECT count(*) FROM memberships") != 2 || count(t, f.db, "SELECT count(*) FROM assignments") != 2 || count(t, f.db, "SELECT count(*) FROM assignments WHERE active") != 1 || count(t, f.db, "SELECT count(*) FROM submissions") != 1 || count(t, f.db, "SELECT count(*) FROM scheduling_audit WHERE action='request-resubmitted'") != 0 {
				t.Fatal("failed batch left partial writes")
			}
		})
	}
	exec(t, f.app, s.Command{Action: "unassign", ID: other, WorkerID: occupant})
	in := f.memberInput()
	in.ShiftIDs = []string{f.shift, other}
	out, err := f.app.Submit(ctx, f.who, in)
	must(t, err)
	if len(out.Receipt.Items) != 2 || out.Receipt.Items[0].Status != "pending" || out.Receipt.Items[1].Status != "pending" || count(t, f.db, "SELECT count(*) FROM requests WHERE worker_id=? AND status='pending'", f.worker) != 2 || count(t, f.db, "SELECT count(*) FROM assignments WHERE active") != 0 || count(t, f.db, "SELECT count(*) FROM memberships") != 2 {
		t.Fatal("valid batch did not create exactly two pending requests", out)
	}
}

func TestMemberHTTPResubmissionRequiresFreshApproval(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "frame"}[hx], func(t *testing.T) {
			f := setupApprovedSignup(t, s.Identity{Issuer: "urn:herbstfest:local", Subject: "developer"})
			exec(t, f.app, s.Command{Action: "unassign", ID: f.shift, WorkerID: f.worker})
			exec(t, f.app, s.Command{Action: "shift-save", ID: f.shift, Title: "Aufbau später", Start: "2027-09-10T10:00:00+02:00", End: "2027-09-10T13:00:00+02:00", Capacity: 1, Version: 1})
			full := exec(t, f.app, s.Command{Action: "shift-save", ParticipationID: f.part, Title: "Voller Dienst", Start: "2027-09-10T13:00:00+02:00", End: "2027-09-10T15:00:00+02:00", Capacity: 1}).ID
			var stand string
			must(t, f.db.Raw("SELECT stand_id FROM participations WHERE id=?", f.part).Scan(&stand).Error)
			occupant := exec(t, f.app, s.Command{Action: "member-add", StandID: stand, FirstName: "Anderer", LastName: "Helfer"}).ID
			exec(t, f.app, s.Command{Action: "assign", ID: full, WorkerID: occupant})
			web := uiHTTP(t, f.app, func() {})
			request := func(method, path string, values url.Values) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				web.ServeHTTP(w, r)
				return w
			}
			values := url.Values{"key": {s.ID() + s.ID()}, "shift": {f.shift, full}, "return_to": {"/requests/mine"}}
			failed := request(http.MethodPost, "/requests", values)
			if failed.Code != http.StatusConflict || !reflect.DeepEqual(f.currentRequest(t), f.request) || count(t, f.db, "SELECT count(*) FROM requests") != 1 || count(t, f.db, "SELECT count(*) FROM submissions") != 1 {
				t.Fatal("member HTTP batch bypassed capacity or partially reopened the request", failed.Code)
			}
			values["shift"] = []string{f.shift}
			for _, newKey := range []bool{false, false, true} {
				if newKey {
					values.Set("key", s.ID()+s.ID())
				}
				posted := request(http.MethodPost, "/requests", values)
				if hx {
					if posted.Code != 200 || !strings.HasPrefix(posted.Header().Get("HX-Replace-Url"), "/requests/mine?") || !strings.Contains(posted.Body.String(), "/requests/"+f.request.ID+"/withdraw") {
						t.Fatal("member frame response did not show the pending request", posted.Code)
					}
				} else if posted.Code != 303 || !strings.HasPrefix(posted.Header().Get("Location"), "/requests/mine?") {
					t.Fatal("member request did not redirect to its receipt", posted.Code)
				}
			}
			own := request(http.MethodGet, "/requests/mine", nil)
			if own.Code != 200 || !strings.Contains(own.Body.String(), "/requests/"+f.request.ID+"/withdraw") || strings.Contains(own.Body.String(), "Dienstzeiten wurden geändert") || f.currentRequest(t).ShiftVersion != 2 {
				t.Fatal("member receipt did not show a fresh pending request", own.Code)
			}
			publicReceipt := request(http.MethodPost, "/register/"+f.original.Token, url.Values{"key": {f.original.Key}, "first_name": {f.original.FirstName}, "last_name": {f.original.LastName}, "shift": {f.shift}})
			if publicReceipt.Code != 200 || !strings.Contains(publicReceipt.Body.String(), "Deine Anfragen sind gespeichert.") || !strings.Contains(publicReceipt.Body.String(), ">Offen</span>") || strings.Contains(publicReceipt.Body.String(), ">Bestätigt</span>") {
				t.Fatal("original HTTP retry did not reflect the pending receipt", publicReceipt.Code)
			}
			if count(t, f.db, "SELECT count(*) FROM assignments WHERE worker_id=? AND active", f.worker) != 0 || count(t, f.db, "SELECT count(*) FROM assignments WHERE worker_id=?", f.worker) != 1 || count(t, f.db, "SELECT count(*) FROM memberships") != 2 || count(t, f.db, "SELECT count(*) FROM requests") != 1 {
				t.Fatal("member retries duplicated data or assigned a place before approval")
			}
			// Grant the browser identity organizer access only after its member submission.
			invite := exec(t, f.app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: f.part}).Token
			_, err := f.app.Execute(ctx, f.who, s.Command{Action: "redeem-organizer", Token: invite})
			must(t, err)
			queuePath := "/requests/pending?stand=" + f.part
			queue := request(http.MethodGet, queuePath, nil)
			if queue.Code != 200 || !strings.Contains(queue.Body.String(), `data-request-id="`+f.request.ID+`"`) || !strings.Contains(queue.Body.String(), "/requests/"+f.request.ID+"/approve") || strings.Contains(queue.Body.String(), "Für frühere Dienstzeiten angefragt") {
				t.Fatal("organizer HTTP queue omitted a current, actionable resubmission", queue.Code)
			}
			approved := request(http.MethodPost, "/requests/"+f.request.ID+"/approve", url.Values{"return_to": {queuePath}})
			wantStatus := http.StatusSeeOther
			if hx {
				wantStatus = http.StatusOK
			}
			if approved.Code != wantStatus || f.currentRequest(t).Status != "approved" || count(t, f.db, "SELECT count(*) FROM assignments WHERE worker_id=? AND active", f.worker) != 1 || count(t, f.db, "SELECT count(*) FROM assignments WHERE worker_id=?", f.worker) != 1 || count(t, f.db, "SELECT count(*) FROM memberships") != 2 {
				t.Fatal("fresh HTTP approval did not restore exactly one assignment", approved.Code)
			}
		})
	}
}
