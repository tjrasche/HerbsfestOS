package scheduling

import (
	"encoding/json"
	"errors"
)

func (t *Tx) memberPermission(stand string) error {
	t.resetFacts()
	t.fact("festival:herbstfest", "festival", "stand:"+stand)
	var p Participation
	if err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.stand_id=? AND p.edition_id=?", stand, t.current).Scan(&p).Error; err != nil {
		return err
	}
	if p.ID != "" {
		if err := t.partFacts(p); err != nil {
			return err
		}
	}
	return t.check("manage_members", "stand:"+stand)
}
func (t *Tx) workers(c Command, out *Result) error {
	switch c.Action {
	case "member-add", "member-remove":
		if err := t.memberPermission(c.StandID); err != nil {
			return err
		}
		if c.Action == "member-add" {
			if c.WorkerID == "" {
				if !textValid(c.FirstName, 120) || !textValid(c.LastName, 120) {
					return ErrInvalid
				}
				c.WorkerID = ID()
				if err := t.db.Exec("INSERT INTO workers(id,first_name,last_name) VALUES(?,?,?)", c.WorkerID, clean(c.FirstName), clean(c.LastName)).Error; err != nil {
					return err
				}
			}
			if err := t.db.Exec("INSERT INTO memberships(stand_id,worker_id) VALUES(?,?) ON CONFLICT DO NOTHING", c.StandID, c.WorkerID).Error; err != nil {
				return err
			}
			out.ID = c.WorkerID
		} else {
			var count int64
			if err := t.db.Raw("SELECT count(*) FROM assignments a JOIN shifts s ON s.id=a.shift_id JOIN participations p ON p.id=s.participation_id WHERE a.worker_id=? AND p.stand_id=? AND a.active AND NOT s.cancelled AND s.ends_at>?", c.WorkerID, c.StandID, t.store.Now()).Scan(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			if err := t.db.Exec("DELETE FROM memberships WHERE stand_id=? AND worker_id=?", c.StandID, c.WorkerID).Error; err != nil {
				return err
			}
		}
	case "worker-save":
		if err := t.permitWorker(c.ID, "edit_contacts"); err != nil {
			return err
		}
		if !textValid(c.FirstName, 120) || !textValid(c.LastName, 120) || len(c.Email) > 320 || len(c.Phone) > 80 || len(c.Notes) > 20000 {
			return ErrInvalid
		}
		var old Worker
		if err := t.db.Raw("SELECT * FROM workers WHERE id=?", c.ID).Scan(&old).Error; err != nil {
			return err
		}
		if old.ID == "" {
			return ErrInvalid
		}
		if c.Notes != old.Notes {
			if err := t.permitWorker(c.ID, "edit_notes"); err != nil {
				return err
			}
		}
		if err := t.db.Exec("UPDATE workers SET first_name=?,last_name=?,email=?,phone=?,notes=? WHERE id=?", clean(c.FirstName), clean(c.LastName), clean(c.Email), clean(c.Phone), c.Notes, c.ID).Error; err != nil {
			return err
		}
	case "worker-merge":
		if err := t.admin("manage_catalog"); err != nil {
			return err
		}
		if !c.Confirm || c.ID == c.OtherWorkerID {
			return ErrInvalid
		}
		if !textValid(c.FirstName, 120) || !textValid(c.LastName, 120) || len(c.Email) > 320 || len(c.Phone) > 80 || len(c.Notes) > 20000 {
			return ErrInvalid
		}
		return t.merge(c.ID, c.OtherWorkerID, c, false)
	default:
		return ErrInvalid
	}
	return t.audit(c.Action, c.WorkerID, "{}")
}

// merge preserves membership rows only when they actually exist; historical
// assignments never manufacture a new membership. Rows coalesce deterministically
// and a full pre-merge snapshot remains in the audit log.
func (t *Tx) merge(target, source string, c Command, fresh bool) error {
	var a, b Worker
	if err := t.db.Raw("SELECT * FROM workers WHERE id=? FOR UPDATE", target).Scan(&a).Error; err != nil {
		return err
	}
	if err := t.db.Raw("SELECT * FROM workers WHERE id=? FOR UPDATE", source).Scan(&b).Error; err != nil {
		return err
	}
	if a.ID == "" || b.ID == "" {
		return ErrInvalid
	}
	if a.AccountID != nil && b.AccountID != nil && *a.AccountID != *b.AccountID {
		return ErrConflict
	}
	if fresh && b.AccountID != nil {
		return ErrConflict
	}

	var reqs []Request
	if err := t.db.Raw("SELECT * FROM requests WHERE worker_id IN (?,?) ORDER BY id", target, source).Scan(&reqs).Error; err != nil {
		return err
	}
	var assignments []struct {
		ShiftID, WorkerID string
		Active            bool
	}
	if err := t.db.Raw("SELECT * FROM assignments WHERE worker_id IN (?,?)", target, source).Scan(&assignments).Error; err != nil {
		return err
	}
	snapshot, _ := json.Marshal(map[string]any{"workers": []Worker{a, b}, "requests": reqs, "assignments": assignments})
	if err := t.audit("worker-merge", target, string(snapshot)); err != nil {
		return err
	}
	if err := t.db.Exec("INSERT INTO memberships(stand_id,worker_id) SELECT stand_id,? FROM memberships WHERE worker_id=? ON CONFLICT DO NOTHING", target, source).Error; err != nil {
		return err
	}
	if err := t.db.Exec("INSERT INTO assignments(shift_id,worker_id,active) SELECT shift_id,?,active FROM assignments WHERE worker_id=? ON CONFLICT(shift_id,worker_id) DO UPDATE SET active=assignments.active OR excluded.active", target, source).Error; err != nil {
		return err
	}
	// Preserve a pending request over a terminal duplicate unless assignment exists.
	if err := t.db.Exec("INSERT INTO requests(id,shift_id,worker_id,status,shift_version,created_at) SELECT id||'-merged',shift_id,?,status,shift_version,created_at FROM requests WHERE worker_id=? ON CONFLICT(shift_id,worker_id) DO UPDATE SET status=CASE WHEN requests.status='approved' OR excluded.status='approved' THEN 'approved' WHEN requests.status='pending' OR excluded.status='pending' THEN 'pending' ELSE requests.status END", target, source).Error; err != nil {
		return err
	}
	if err := t.db.Exec("DELETE FROM requests WHERE worker_id=?", source).Error; err != nil {
		return err
	}
	if err := t.db.Exec("DELETE FROM assignments WHERE worker_id=?", source).Error; err != nil {
		return err
	}
	if err := t.db.Exec("DELETE FROM memberships WHERE worker_id=?", source).Error; err != nil {
		return err
	}
	// Invalidate outstanding ownership proofs for both records during a historical merge.
	if !fresh {
		if err := t.db.Exec("UPDATE capabilities SET disabled=true WHERE worker_id IN (?,?) AND kind IN ('connection','fresh')", target, source).Error; err != nil {
			return err
		}
	}
	if err := t.db.Exec("UPDATE capabilities SET worker_id=? WHERE worker_id=?", target, source).Error; err != nil {
		return err
	}
	if err := t.db.Exec("UPDATE submissions SET worker_id=? WHERE worker_id=?", target, source).Error; err != nil {
		return err
	}
	if b.AccountID != nil && a.AccountID == nil {
		if err := t.db.Exec("UPDATE workers SET account_id=NULL WHERE id=?", source).Error; err != nil {
			return err
		}
		if err := t.db.Exec("UPDATE workers SET account_id=? WHERE id=?", *b.AccountID, target).Error; err != nil {
			return err
		}
	}
	if !fresh {
		if err := t.db.Exec("UPDATE workers SET first_name=?,last_name=?,email=?,phone=?,notes=? WHERE id=?", clean(c.FirstName), clean(c.LastName), clean(c.Email), clean(c.Phone), c.Notes, target).Error; err != nil {
			return err
		}
	}
	return t.db.Exec("DELETE FROM workers WHERE id=?", source).Error
}

func (t *Tx) connectionPermission(worker string) error {
	if err := t.permitWorker(worker, "edit_notes"); err == nil || !errors.Is(err, ErrDenied) {
		return err
	}
	// An organizer may also connect a worker with a request at their current stand.
	var ps []Participation
	if err := t.db.Raw("SELECT DISTINCT p.* FROM participations p JOIN shifts s ON s.participation_id=p.id JOIN requests r ON r.shift_id=s.id JOIN teams tm ON tm.participation_id=p.id WHERE r.worker_id=? AND p.edition_id=? AND p.enabled AND tm.account_id=?", worker, t.current, t.account).Scan(&ps).Error; err != nil {
		return err
	}
	for _, p := range ps {
		err := t.permitPart(p, "edit_schedule")
		if err == nil || !errors.Is(err, ErrDenied) {
			return err
		}
	}
	return ErrDenied
}
