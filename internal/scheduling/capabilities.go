package scheduling

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

func (t *Tx) capability(token string) (Capability, error) {
	var c Capability
	err := t.db.Raw("SELECT * FROM capabilities WHERE digest=?", digest(token)).Scan(&c).Error
	if err != nil {
		return c, err
	}
	if c.Digest == "" || c.Disabled {
		return c, ErrDenied
	}
	return c, nil
}
func (t *Tx) issue(c Command, out *Result) error {
	var err error
	var worker, part, shift, issuer any
	var expiry any
	kind := ""
	scope := ""
	switch c.Action {
	case "signup-link":
		p, err := t.participation(c.ParticipationID)
		if err != nil {
			return err
		}
		// Global organizers may manage links without a team; schedule edits still
		// require an explicit team. Non-globals are restricted to current teams.
		if t.global {
			err = t.admin("manage_catalog")
		} else {
			err = t.permitPart(p, "edit_schedule")
		}
		if err != nil {
			return err
		}
		part = p.ID
		kind = "signup"
		if c.ID != "" {
			s, sp, err := t.shift(c.ID, false)
			if err != nil {
				return err
			}
			if sp.ID != p.ID {
				return ErrInvalid
			}
			shift = s.ID
		}
		if err = t.db.Exec("UPDATE capabilities SET disabled=true WHERE kind='signup' AND participation_id=? AND shift_id IS NOT DISTINCT FROM ?", part, shift).Error; err != nil {
			return err
		}
	case "connection-link":
		if err = t.connectionPermission(c.WorkerID); err != nil {
			return err
		}
		var w Worker
		if err = t.db.Raw("SELECT * FROM workers WHERE id=?", c.WorkerID).Scan(&w).Error; err != nil {
			return err
		}
		if w.ID == "" || w.AccountID != nil {
			return ErrConflict
		}
		kind = "connection"
		worker = c.WorkerID
		expiry = t.store.Now().Add(7 * 24 * time.Hour)
		if err = t.db.Exec("UPDATE capabilities SET disabled=true WHERE worker_id=? AND kind='connection'", worker).Error; err != nil {
			return err
		}
	case "organizer-invite":
		if err = t.admin("issue_organizer_invite"); err != nil {
			return err
		}
		if c.Scope != "global" && c.Scope != "team" {
			return ErrInvalid
		}
		scope = c.Scope
		kind = "organizer"
		issuer = t.account
		expiry = t.store.Now().Add(7 * 24 * time.Hour)
		if scope == "team" {
			if _, err = t.participation(c.ParticipationID); err != nil {
				return err
			}
			part = c.ParticipationID
		}
		if c.Token != "" {
			old, err := t.capability(c.Token)
			if err != nil {
				return err
			}
			if old.Kind != "organizer" || old.Scope != scope || old.ConsumedBy != nil {
				return ErrInvalid
			}
			if (old.ParticipationID == nil) != (part == nil) || old.ParticipationID != nil && *old.ParticipationID != part {
				return ErrInvalid
			}
			if err = t.db.Exec("UPDATE capabilities SET disabled=true WHERE digest=?", old.Digest).Error; err != nil {
				return err
			}
		}
	case "capability-revoke":
		old, err := t.capability(c.Token)
		if err != nil {
			return err
		}
		switch old.Kind {
		case "organizer":
			err = t.admin("issue_organizer_invite")
		case "connection", "fresh":
			if old.WorkerID == nil {
				return ErrInvalid
			}
			err = t.connectionPermission(*old.WorkerID)
		case "signup":
			if old.ParticipationID == nil {
				return ErrInvalid
			}
			p, e := t.participation(*old.ParticipationID)
			if e != nil {
				return e
			}
			if t.global {
				err = t.admin("manage_catalog")
			} else {
				err = t.permitPart(p, "edit_schedule")
			}
		default:
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		return t.db.Exec("UPDATE capabilities SET disabled=true WHERE digest=?", old.Digest).Error
	default:
		return ErrInvalid
	}
	token := ID() + ID()
	if err = t.db.Exec("INSERT INTO capabilities(digest,kind,participation_id,shift_id,worker_id,issuer_id,scope,expires_at) VALUES(?,?,?,?,?,?,?,?)", digest(token), kind, part, shift, worker, issuer, scope, expiry).Error; err != nil {
		return err
	}
	out.Token = token
	return t.audit(c.Action, kind, "{}")
}
func (t *Tx) redeem(who Identity, c Command, out *Result) error {
	cap, err := t.capability(c.Token)
	if err != nil {
		return err
	}
	if cap.ExpiresAt == nil || !t.store.Now().Before(*cap.ExpiresAt) {
		return ErrDenied
	}
	// Engine availability is mandatory even for bearer-proof operations.
	t.resetFacts()
	t.fact(t.principal(), "owner", "worker:proof")
	if err = t.check("view_details", "worker:proof"); err != nil {
		return err
	}
	account, err := t.ensureAccount(who)
	if err != nil {
		return err
	}
	if cap.ConsumedBy != nil {
		if *cap.ConsumedBy == account {
			return nil
		}
		return ErrConflict
	}
	if c.Action == "redeem-organizer" {
		if cap.Kind != "organizer" || cap.IssuerID == nil {
			return ErrDenied
		}
		var issuerGlobal bool
		if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM global_grants WHERE account_id=?)", *cap.IssuerID).Scan(&issuerGlobal).Error; err != nil {
			return err
		}
		if !issuerGlobal {
			return ErrDenied
		}
		if cap.Scope == "global" {
			err = t.db.Exec("INSERT INTO global_grants(account_id) VALUES(?) ON CONFLICT DO NOTHING", account).Error
		} else if cap.Scope == "team" && cap.ParticipationID != nil {
			err = t.db.Exec("INSERT INTO teams(participation_id,account_id) VALUES(?,?) ON CONFLICT DO NOTHING", *cap.ParticipationID, account).Error
		} else {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
	} else {
		if (cap.Kind != "fresh" && cap.Kind != "connection") || cap.WorkerID == nil {
			return ErrDenied
		}
		var worker Worker
		if err = t.db.Raw("SELECT * FROM workers WHERE id=? FOR UPDATE", *cap.WorkerID).Scan(&worker).Error; err != nil {
			return err
		}
		if worker.ID == "" {
			return ErrConflict
		}
		if worker.AccountID != nil && *worker.AccountID != account {
			return ErrConflict
		}
		var existing string
		if err = t.db.Raw("SELECT id FROM workers WHERE account_id=?", account).Scan(&existing).Error; err != nil {
			return err
		}
		if existing != "" && existing != worker.ID {
			if cap.Kind != "fresh" {
				return ErrConflict
			}
			if err = t.merge(existing, worker.ID, Command{}, true); err != nil {
				return err
			}
			out.ID = existing
		} else {
			if err = t.db.Exec("UPDATE workers SET account_id=? WHERE id=?", account, worker.ID).Error; err != nil {
				return err
			}
			out.ID = worker.ID
		}
	}
	if err = t.db.Exec("UPDATE capabilities SET consumed_by=? WHERE digest=?", account, cap.Digest).Error; err != nil {
		return err
	}
	return t.audit(c.Action, cap.Kind, "{}")
}

type SignupInput struct {
	Token, Key, FirstName, LastName string
	ShiftIDs                        []string
}
type Submission struct{ WorkerID, ConnectionToken string }

func (s *Store) Submit(ctx context.Context, who Identity, in SignupInput) (Submission, error) {
	var out Submission
	err := s.transact(ctx, who, true, func(t *Tx) error {
		if len(in.Key) < 32 || len(in.Key) > 100 || !textValid(in.FirstName, 120) || !textValid(in.LastName, 120) || len(in.ShiftIDs) == 0 || len(in.ShiftIDs) > 150 {
			return ErrInvalid
		}
		ids := append([]string(nil), in.ShiftIDs...)
		sort.Strings(ids)
		for i := 1; i < len(ids); i++ {
			if ids[i] == ids[i-1] {
				return ErrInvalid
			}
		}
		data, _ := json.Marshal([]any{digest(in.Token), clean(in.FirstName), clean(in.LastName), ids, t.account})
		fingerprint := digest(string(data))
		var old struct{ Fingerprint, WorkerID, ConnectionToken string }
		if err := t.db.Raw("SELECT fingerprint,worker_id,connection_token FROM submissions WHERE key=?", digest(in.Key)).Scan(&old).Error; err != nil {
			return err
		}
		if old.Fingerprint != "" {
			if old.Fingerprint != fingerprint {
				return ErrConflict
			}
			// Retry remains available after public expiry without exposing roster data.
			t.resetFacts()
			t.fact(t.principal(), "owner", "worker:proof")
			if err := t.check("view_details", "worker:proof"); err != nil {
				return err
			}
			out = Submission{old.WorkerID, old.ConnectionToken}
			return nil
		}
		var cap Capability
		var err error
		if in.Token != "" {
			cap, err = t.capability(in.Token)
			if err != nil {
				return err
			}
			if cap.Kind != "signup" || cap.ParticipationID == nil {
				return ErrDenied
			}
		}
		shifts := make([]Shift, 0, len(ids))
		for _, id := range ids {
			sh, p, err := t.shift(id, true)
			if err != nil {
				return err
			}
			if in.Token != "" {
				if p.ID != *cap.ParticipationID || cap.ShiftID != nil && *cap.ShiftID != sh.ID {
					return ErrDenied
				}
			} else {
				var member bool
				if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM memberships WHERE stand_id=? AND worker_id=?)", p.StandID, t.worker).Scan(&member).Error; err != nil {
					return err
				}
				if !member || t.worker == "" {
					return ErrDenied
				}
			}
			if err = t.signupPermit(sh, p, "request_place"); err != nil {
				return err
			}
			var count int64
			if err = t.db.Raw("SELECT count(*) FROM assignments WHERE shift_id=? AND active", sh.ID).Scan(&count).Error; err != nil {
				return err
			}
			if sh.Cancelled || !t.store.Now().Before(sh.StartsAt) || count >= int64(sh.Capacity) {
				return ErrConflict
			}
			shifts = append(shifts, sh)
		}
		worker := t.worker
		if worker == "" {
			worker = ID()
			if err = t.db.Exec("INSERT INTO workers(id,first_name,last_name) VALUES(?,?,?)", worker, clean(in.FirstName), clean(in.LastName)).Error; err != nil {
				return err
			}
		}
		for _, sh := range shifts {
			var r Request
			if err = t.db.Raw("SELECT * FROM requests WHERE shift_id=? AND worker_id=?", sh.ID, worker).Scan(&r).Error; err != nil {
				return err
			}
			if r.ID != "" {
				if r.Status == "pending" || r.Status == "approved" {
					continue
				}
				if err = t.audit("request-resubmitted", r.ID, "{}"); err != nil {
					return err
				}
				if err = t.db.Exec("UPDATE requests SET status='pending',shift_version=?,created_at=? WHERE id=?", sh.Version, t.store.Now(), r.ID).Error; err != nil {
					return err
				}
			} else {
				if err = t.db.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version) VALUES(?,?,?,'pending',?)", ID(), sh.ID, worker, sh.Version).Error; err != nil {
					return err
				}
			}
		}
		proof := ID() + ID()
		if t.worker == "" {
			if err = t.db.Exec("INSERT INTO capabilities(digest,kind,worker_id,expires_at) VALUES(?,'fresh',?,?)", digest(proof), worker, t.store.Now().Add(7*24*time.Hour)).Error; err != nil {
				return err
			}
		} else {
			proof = ""
		}
		if err = t.db.Exec("INSERT INTO submissions(key,fingerprint,worker_id,connection_token) VALUES(?,?,?,?)", digest(in.Key), fingerprint, worker, proof).Error; err != nil {
			return err
		}
		out = Submission{worker, proof}
		return t.audit("submit", worker, "{}")
	})
	return out, err
}
func (t *Tx) signupPermit(sh Shift, p Participation, permission string) error {
	e, err := t.edition(p.EditionID)
	if err != nil {
		return err
	}
	if !t.store.Now().Before(day(e.EndDay).AddDate(0, 0, 1)) {
		return ErrDenied
	}
	t.resetFacts()
	if err = t.partFacts(p); err != nil {
		return err
	}
	t.fact("participation:"+p.ID, "participation", "shift:"+sh.ID)
	t.fact(t.principal(), "signup", "shift:"+sh.ID)
	return t.check(permission, "shift:"+sh.ID)
}
