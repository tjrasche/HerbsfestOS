package scheduling_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	s "github.com/tjrasche/HerbsfestOS/internal/scheduling"
)

func TestConnectionReplacementInvalidatesAllOwnershipProofs(t *testing.T) {
	for _, action := range []string{"connection-link", "reissue-fresh", "reissue-connection"} {
		t.Run(action, func(t *testing.T) {
			app, db := setup(t)
			_, part, shift := baseline(t, app)
			signup := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part})
			submit := func(first string) s.Submission {
				v, err := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: signup.Token, Key: s.ID() + s.ID(), FirstName: first, LastName: "Gast", ShiftIDs: []string{shift}})
				must(t, err)
				return v
			}
			worker := submit("Replace")
			old := exec(t, app, s.Command{Action: "connection-link", WorkerID: worker.WorkerID})
			// Existing installations can have both proofs active from before replacement
			// invalidated signup-issued ownership proofs as well as connection links.
			must(t, db.Exec("UPDATE capabilities SET disabled=false WHERE worker_id=? AND kind='fresh'", worker.WorkerID).Error)
			var freshID string
			must(t, db.Raw("SELECT digest FROM capabilities WHERE worker_id=? AND kind='fresh'", worker.WorkerID).Scan(&freshID).Error)
			otherFresh := submit("Other fresh")
			otherWorker := submit("Other connection")
			otherConnection := exec(t, app, s.Command{Action: "connection-link", WorkerID: otherWorker.WorkerID})
			invite := exec(t, app, s.Command{Action: "organizer-invite", Scope: "team", ParticipationID: part})
			private, public := http.NewServeMux(), http.NewServeMux()
			s.NewHandler(app).Register(private, public, func(h http.Handler) http.Handler { return h })
			getProof := func(token string, want int) {
				t.Helper()
				w := httptest.NewRecorder()
				public.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/proof/"+token, nil))
				if w.Code != want {
					t.Fatalf("proof GET status %d, want %d", w.Code, want)
				}
				if want == http.StatusForbidden && strings.Contains(w.Body.String(), "Replace Gast") {
					t.Fatal("replaced proof still disclosed the worker")
				}
			}
			for _, token := range []string{worker.ConnectionToken, old.Token} {
				getProof(token, http.StatusOK)
			}
			command := s.Command{Action: "connection-link", WorkerID: worker.WorkerID}
			if action != "connection-link" {
				command = s.Command{Action: "capability-reissue", CapabilityID: old.ID}
				if action == "reissue-fresh" {
					command.CapabilityID = freshID
				}
			}
			replacement := exec(t, app, command)
			if count(t, db, "SELECT count(*) FROM capabilities WHERE worker_id=? AND kind IN ('fresh','connection') AND NOT disabled", worker.WorkerID) != 1 {
				t.Fatal("replacement left another ownership proof active")
			}
			staleHolder := s.Identity{Issuer: "test:issuer", Subject: "stale-holder"}
			for _, token := range []string{worker.ConnectionToken, old.Token} {
				getProof(token, http.StatusForbidden)
				if _, err := app.InspectProof(ctx, token); !errors.Is(err, s.ErrDenied) {
					t.Fatal("replaced proof remained inspectable", err)
				}
				if _, err := app.Execute(ctx, staleHolder, s.Command{Action: "connect", Token: token}); !errors.Is(err, s.ErrDenied) {
					t.Fatal("replaced proof still bound the worker", err)
				}
			}
			if count(t, db, "SELECT count(*) FROM workers WHERE id=? AND account_id IS NOT NULL", worker.WorkerID) != 0 || count(t, db, "SELECT count(*) FROM accounts WHERE subject=?", staleHolder.Subject) != 0 {
				t.Fatal("denied redemption changed account ownership")
			}
			for _, token := range []string{otherFresh.ConnectionToken, otherConnection.Token, invite.Token} {
				getProof(token, http.StatusOK)
			}
			_, err := app.Signup(ctx, signup.Token)
			must(t, err)
			getProof(replacement.Token, http.StatusOK)
			newHolder := s.Identity{Issuer: "test:issuer", Subject: "replacement-holder"}
			_, err = app.Execute(ctx, newHolder, s.Command{Action: "connect", Token: replacement.Token})
			must(t, err)
			if count(t, db, "SELECT count(*) FROM workers w JOIN accounts a ON a.id=w.account_id WHERE w.id=? AND a.subject=?", worker.WorkerID, newHolder.Subject) != 1 {
				t.Fatal("replacement did not bind the intended account")
			}
			if _, err = app.Execute(ctx, staleHolder, s.Command{Action: "connect", Token: replacement.Token}); !errors.Is(err, s.ErrConflict) {
				t.Fatal("replacement allowed a second account to take ownership", err)
			}
		})
	}
}

func TestConnectionReplacementFailurePreservesExistingProof(t *testing.T) {
	for _, action := range []string{"connection-link", "capability-reissue"} {
		t.Run(action, func(t *testing.T) {
			app, db := setup(t)
			_, part, shift := baseline(t, app)
			signup := exec(t, app, s.Command{Action: "signup-link", ParticipationID: part})
			worker, err := app.Submit(ctx, s.Identity{}, s.SignupInput{Token: signup.Token, Key: s.ID() + s.ID(), FirstName: "Keep", LastName: "Gast", ShiftIDs: []string{shift}})
			must(t, err)
			command := s.Command{Action: action, WorkerID: worker.WorkerID}
			if action == "capability-reissue" {
				must(t, db.Raw("SELECT digest FROM capabilities WHERE worker_id=? AND kind='fresh'", worker.WorkerID).Scan(&command.CapabilityID).Error)
			}
			before := count(t, db, "SELECT count(*) FROM capabilities")
			auditBefore := count(t, db, "SELECT count(*) FROM scheduling_audit")
			revisionBefore := count(t, db, "SELECT revision FROM scheduling_state WHERE id=1")
			unauthorized := s.Identity{Issuer: "test:issuer", Subject: "unauthorized-replacement"}
			if _, err = app.Execute(ctx, unauthorized, command); !errors.Is(err, s.ErrDenied) {
				t.Fatal("unauthorized replacement was not denied", err)
			}
			// Reject the new connection proof after the old fresh proof was disabled,
			// so the failure exercises rollback of the replacement's earlier writes.
			must(t, db.Exec("ALTER TABLE capabilities ADD CONSTRAINT reject_connection_proof CHECK (kind <> 'connection') NOT VALID").Error)
			if _, err = app.Execute(ctx, admin, command); err == nil {
				t.Fatal("forced replacement failure was ignored")
			}
			if count(t, db, "SELECT count(*) FROM capabilities") != before || count(t, db, "SELECT count(*) FROM scheduling_audit") != auditBefore || count(t, db, "SELECT revision FROM scheduling_state WHERE id=1") != revisionBefore {
				t.Fatal("failed replacement committed partial writes")
			}
			if kind, err := app.InspectProof(ctx, worker.ConnectionToken); err != nil || kind != "fresh" {
				t.Fatal("failed replacement invalidated the existing proof", kind, err)
			}
			holder := s.Identity{Issuer: "test:issuer", Subject: "original-holder"}
			_, err = app.Execute(ctx, holder, s.Command{Action: "connect", Token: worker.ConnectionToken})
			must(t, err)
			if count(t, db, "SELECT count(*) FROM workers w JOIN accounts a ON a.id=w.account_id WHERE w.id=? AND a.subject=?", worker.WorkerID, holder.Subject) != 1 {
				t.Fatal("existing proof could not bind after failed replacement")
			}
		})
	}
}
