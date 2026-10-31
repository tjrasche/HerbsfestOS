package scheduling

import (
	"context"
	"encoding/json"
	"time"
)

type Command struct {
	Action, ID, ParticipationID, EditionID, SourceID, StandID, WorkerID, OtherWorkerID, AccountID            string
	Label, StartDay, EndDay, Name, Title, Start, End, FirstName, LastName, Email, Phone, Notes, Token, Scope string
	Capacity, Version                                                                                        int
	Enabled, Confirm                                                                                         bool
}
type Result struct{ ID, Token, Message string }

// Execute exposes one domain command boundary shared by HTTP and integration
// tests. Every command resolves its resource from PostgreSQL inside the lock.
func (s *Store) Execute(ctx context.Context, who Identity, c Command) (Result, error) {
	var out Result
	err := s.transact(ctx, who, true, func(t *Tx) error {
		var err error
		switch c.Action {
		case "edition-save":
			if err = t.admin("manage_catalog"); err != nil {
				return err
			}
			if !validDates(c.Label, c.StartDay, c.EndDay) {
				return ErrInvalid
			}
			if c.ID == "" {
				c.ID = ID()
				if err = t.db.Exec("INSERT INTO editions(id,label,start_day,end_day) VALUES(?,?,?::date,?::date)", c.ID, clean(c.Label), c.StartDay, c.EndDay).Error; err != nil {
					return err
				}
				source := c.SourceID
				if source == "" {
					source = t.current
				}
				if source != "" {
					if _, err = t.edition(source); err != nil {
						return err
					}
					var ps []Participation
					if err = t.db.Raw("SELECT * FROM participations WHERE edition_id=? AND enabled ORDER BY id", source).Scan(&ps).Error; err != nil {
						return err
					}
					for _, p := range ps {
						id := ID()
						if err = t.db.Exec("INSERT INTO participations(id,edition_id,stand_id) VALUES(?,?,?)", id, c.ID, p.StandID).Error; err != nil {
							return err
						}
						if err = t.db.Exec("INSERT INTO teams(participation_id,account_id) SELECT ?,account_id FROM teams WHERE participation_id=?", id, p.ID).Error; err != nil {
							return err
						}
					}
				}
			} else {
				if _, err = t.edition(c.ID); err != nil {
					return err
				}
				if err = t.db.Exec("UPDATE editions SET label=?,start_day=?::date,end_day=?::date WHERE id=?", clean(c.Label), c.StartDay, c.EndDay, c.ID).Error; err != nil {
					return err
				}
			}
			out.ID = c.ID
			out.Message = "Ausgabe gespeichert. Abweichende Dienste bleiben erhalten und sind markiert."
		case "activate":
			if err = t.admin("activate_edition"); err != nil {
				return err
			}
			if err = t.ready(c.ID); err != nil {
				return err
			}
			if !c.Confirm {
				return ErrInvalid
			}
			if err = t.db.Exec("UPDATE scheduling_state SET current_edition=? WHERE id=1", c.ID).Error; err != nil {
				return err
			}
			t.current = c.ID
		case "stand-save":
			if err = t.admin("manage_catalog"); err != nil {
				return err
			}
			if !textValid(c.Name, 120) {
				return ErrInvalid
			}
			if c.ID == "" {
				c.ID = ID()
				err = t.db.Exec("INSERT INTO stands(id,name) VALUES(?,?)", c.ID, clean(c.Name)).Error
			} else {
				err = t.db.Exec("UPDATE stands SET name=? WHERE id=?", clean(c.Name), c.ID).Error
			}
			if err != nil {
				return err
			}
			out.ID = c.ID
		case "participation":
			if err = t.admin("manage_catalog"); err != nil {
				return err
			}
			if c.ID == "" {
				c.ID = ID()
				if err = t.db.Exec("INSERT INTO participations(id,edition_id,stand_id,enabled) VALUES(?,?,?,?)", c.ID, c.EditionID, c.StandID, c.Enabled).Error; err != nil {
					return err
				}
			} else {
				if _, err = t.participation(c.ID); err != nil {
					return err
				}
				if err = t.db.Exec("UPDATE participations SET enabled=? WHERE id=?", c.Enabled, c.ID).Error; err != nil {
					return err
				}
			}
			out.ID = c.ID
		case "team-add", "team-remove", "global-remove":
			if err = t.admin("manage_teams"); err != nil {
				return err
			}
			if c.Action == "team-add" {
				err = t.db.Exec("INSERT INTO teams(participation_id,account_id) VALUES(?,?) ON CONFLICT DO NOTHING", c.ParticipationID, c.AccountID).Error
			}
			if c.Action == "team-remove" {
				err = t.db.Exec("DELETE FROM teams WHERE participation_id=? AND account_id=?", c.ParticipationID, c.AccountID).Error
			}
			if c.Action == "global-remove" {
				var n int64
				if err = t.db.Raw("SELECT count(*) FROM global_grants WHERE account_id<>?", c.AccountID).Scan(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return ErrConflict
				}
				err = t.db.Exec("DELETE FROM global_grants WHERE account_id=?", c.AccountID).Error
			}
			if err != nil {
				return err
			}
		case "shift-save", "shift-cancel", "assign", "unassign", "approve", "decline":
			return t.schedule(c, &out)
		case "member-add", "member-remove", "worker-save", "worker-merge":
			return t.workers(c, &out)
		case "signup-link", "connection-link", "organizer-invite", "capability-revoke":
			return t.issue(c, &out)
		case "redeem-organizer", "connect":
			return t.redeem(who, c, &out)
		case "withdraw":
			return t.withdraw(c.ID)
		default:
			return ErrInvalid
		}
		if err = t.maintainReady(); err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]string{"id": c.ID})
		return t.audit(c.Action, c.ID, string(details))
	})
	return out, err
}

func (t *Tx) schedule(c Command, out *Result) error {
	var sh Shift
	var p Participation
	var err error
	if c.Action == "shift-save" && c.ID == "" {
		p, err = t.participation(c.ParticipationID)
	} else {
		if c.Action == "approve" || c.Action == "decline" {
			var r Request
			if err = t.db.Raw("SELECT * FROM requests WHERE id=?", c.ID).Scan(&r).Error; err != nil {
				return err
			}
			if r.ID == "" {
				return ErrInvalid
			}
			c.WorkerID = r.WorkerID
			c.OtherWorkerID = r.ShiftID
			sh, p, err = t.shift(r.ShiftID, true)
		} else {
			sh, p, err = t.shift(c.ID, true)
		}
	}
	if err != nil {
		return err
	}
	if err = t.permitPart(p, "edit_schedule"); err != nil {
		return err
	}
	switch c.Action {
	case "shift-save":
		start, e := time.Parse(time.RFC3339, c.Start)
		end, f := time.Parse(time.RFC3339, c.End)
		if e != nil || f != nil || !textValid(c.Title, 120) || c.Capacity < 1 {
			return ErrInvalid
		}
		edition, err := t.edition(p.EditionID)
		if err != nil {
			return err
		}
		candidate := Shift{StartsAt: start, EndsAt: end}
		if !candidate.Within(edition) {
			return ErrInvalid
		}
		if sh.ID == "" {
			c.ID = ID()
			err = t.db.Exec("INSERT INTO shifts(id,participation_id,title,starts_at,ends_at,capacity) VALUES(?,?,?,?,?,?)", c.ID, p.ID, clean(c.Title), start, end, c.Capacity).Error
		} else {
			if sh.Cancelled || c.Version != sh.Version {
				return ErrConflict
			}
			var count int64
			if err = t.db.Raw("SELECT count(*) FROM assignments WHERE shift_id=? AND active", sh.ID).Scan(&count).Error; err != nil {
				return err
			}
			if int64(c.Capacity) < count {
				return ErrConflict
			}
			changed := !sh.StartsAt.Equal(start) || !sh.EndsAt.Equal(end)
			if changed && count > 0 && !c.Confirm {
				return ErrConflict
			}
			version := sh.Version
			if changed {
				version++
			}
			err = t.db.Exec("UPDATE shifts SET title=?,starts_at=?,ends_at=?,capacity=?,version=? WHERE id=?", clean(c.Title), start, end, c.Capacity, version, sh.ID).Error
			if err == nil && changed && end.After(t.store.Now()) {
				err = t.db.Exec("INSERT INTO memberships(stand_id,worker_id) SELECT ?,worker_id FROM assignments WHERE shift_id=? AND active ON CONFLICT DO NOTHING", p.StandID, sh.ID).Error
			}
		}
		if err != nil {
			return err
		}
		out.ID = c.ID
	case "shift-cancel":
		if !c.Confirm {
			return ErrInvalid
		}
		if err = t.db.Exec("UPDATE shifts SET cancelled=true,version=version+1 WHERE id=? AND NOT cancelled", sh.ID).Error; err != nil {
			return err
		}
		if err = t.db.Exec("UPDATE assignments SET active=false WHERE shift_id=?", sh.ID).Error; err != nil {
			return err
		}
		if err = t.db.Exec("UPDATE requests SET status='cancelled' WHERE shift_id=? AND status='pending'", sh.ID).Error; err != nil {
			return err
		}
	case "approve", "decline":
		var r Request
		if err = t.db.Raw("SELECT * FROM requests WHERE id=? FOR UPDATE", c.ID).Scan(&r).Error; err != nil {
			return err
		}
		desired := "approved"
		if c.Action == "decline" {
			desired = "declined"
		}
		if r.Status == desired {
			return nil
		}
		if r.Status != "pending" {
			return ErrConflict
		}
		if c.Action == "approve" {
			if err = t.assign(sh, p, r.WorkerID); err != nil {
				return err
			}
		}
		if err = t.db.Exec("UPDATE requests SET status=? WHERE id=?", desired, r.ID).Error; err != nil {
			return err
		}
	case "assign":
		if err = t.assign(sh, p, c.WorkerID); err != nil {
			return err
		}
		if err = t.db.Exec("UPDATE requests SET status='approved' WHERE shift_id=? AND worker_id=? AND status='pending'", sh.ID, c.WorkerID).Error; err != nil {
			return err
		}
	case "unassign":
		if err = t.db.Exec("UPDATE assignments SET active=false WHERE shift_id=? AND worker_id=?", sh.ID, c.WorkerID).Error; err != nil {
			return err
		}
	}
	return t.audit(c.Action, sh.ID, "{}")
}
func (t *Tx) assign(sh Shift, p Participation, worker string) error {
	if sh.Cancelled {
		return ErrConflict
	}
	var active bool
	if err := t.db.Raw("SELECT EXISTS(SELECT 1 FROM assignments WHERE shift_id=? AND worker_id=? AND active)", sh.ID, worker).Scan(&active).Error; err != nil {
		return err
	}
	if active {
		return nil
	}
	var count int64
	if err := t.db.Raw("SELECT count(*) FROM assignments WHERE shift_id=? AND active", sh.ID).Scan(&count).Error; err != nil {
		return err
	}
	if count >= int64(sh.Capacity) {
		return ErrConflict
	}
	if err := t.db.Exec("INSERT INTO assignments(shift_id,worker_id) VALUES(?,?) ON CONFLICT(shift_id,worker_id) DO UPDATE SET active=true", sh.ID, worker).Error; err != nil {
		return err
	}
	if sh.EndsAt.After(t.store.Now()) {
		if err := t.db.Exec("INSERT INTO memberships(stand_id,worker_id) VALUES(?,?) ON CONFLICT DO NOTHING", p.StandID, worker).Error; err != nil {
			return err
		}
	}
	return nil
}
func (t *Tx) withdraw(id string) error {
	var r Request
	if err := t.db.Raw("SELECT * FROM requests WHERE id=?", id).Scan(&r).Error; err != nil {
		return err
	}
	if r.ID == "" {
		return ErrInvalid
	}
	sh, p, err := t.shift(r.ShiftID, true)
	if err != nil {
		return err
	}
	t.resetFacts()
	if err = t.partFacts(p); err != nil {
		return err
	}
	t.workerFacts(r.WorkerID)
	t.fact("participation:"+p.ID, "participation", "shift:"+sh.ID)
	t.fact("worker:"+r.WorkerID, "worker", "request:"+r.ID)
	t.fact("shift:"+sh.ID, "shift", "request:"+r.ID)
	if err = t.check("withdraw", "request:"+r.ID); err != nil {
		return err
	}
	if r.Status == "withdrawn" {
		return nil
	}
	if r.Status != "pending" {
		return ErrConflict
	}
	if err = t.db.Exec("UPDATE requests SET status='withdrawn' WHERE id=?", r.ID).Error; err != nil {
		return err
	}
	return t.audit("withdraw", id, "{}")
}
