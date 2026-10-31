package scheduling

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Person struct{ ID, Name string }
type RequestView struct {
	Request
	Name, Time, Title, Edition string
	CanWithdraw, Stale         bool
}
type ShiftView struct {
	Shift
	Roster           []Person
	Pending          []RequestView
	Outside, Overlap bool
	Free             int
}
type Schedule struct {
	Participation
	Edition                   Edition
	CanEdit, CanManageMembers bool
	Shifts                    []ShiftView
	Members                   []Person
	Team                      []Person
}
type Note struct {
	ID        uint64
	Title     string
	CreatedAt time.Time
}
type View struct {
	AllWorkers                             []Person
	Global                                 bool
	AccountID, WorkerID, Selected, Current string
	Editions                               []Edition
	Stands                                 []Person
	Accounts, Globals                      []Person
	Schedules                              []Schedule
	OwnRequests                            []RequestView
	OwnWorker                              *Worker
	Notes                                  []Note
}

func (s *Store) View(ctx context.Context, who Identity, selected string) (View, error) {
	var v View
	err := s.transact(ctx, who, false, func(t *Tx) error {
		// A harmless self-resource check detects engine outages even for a signed-in
		// identity with no grants, without using display fields as permission facts.
		t.fact(t.principal(), "owner", "worker:availability")
		if err := t.check("view_details", "worker:availability"); err != nil {
			return err
		}
		v.Global = t.global
		v.AccountID = t.account
		v.WorkerID = t.worker
		v.Current = t.current
		v.Selected = t.current
		if selected != "" && selected != t.current {
			if err := t.admin("select_edition"); err != nil {
				return err
			}
			v.Selected = selected
		}
		if t.global {
			if err := t.db.Raw("SELECT id,first_name||' '||last_name AS name FROM workers ORDER BY last_name,first_name,id").Scan(&v.AllWorkers).Error; err != nil {
				return err
			}
			if err := t.admin("manage_catalog"); err != nil {
				return err
			}
			if err := t.db.Raw("SELECT * FROM editions ORDER BY start_day DESC,label").Scan(&v.Editions).Error; err != nil {
				return err
			}
			if err := t.db.Raw("SELECT id,name FROM stands ORDER BY name,id").Scan(&v.Stands).Error; err != nil {
				return err
			}
			if err := t.db.Raw("SELECT id,subject AS name FROM accounts ORDER BY subject,id").Scan(&v.Accounts).Error; err != nil {
				return err
			}
			if err := t.db.Raw("SELECT a.id,a.subject AS name FROM accounts a JOIN global_grants g ON g.account_id=a.id ORDER BY a.id").Scan(&v.Globals).Error; err != nil {
				return err
			}
		}
		if t.worker != "" {
			if err := t.permitWorker(t.worker, "view_details"); err != nil {
				return err
			}
			var worker Worker
			if err := t.db.Raw("SELECT * FROM workers WHERE id=?", t.worker).Scan(&worker).Error; err != nil {
				return err
			}
			v.OwnWorker = &worker
			if err := t.db.Raw("SELECT r.*,s.title,e.label AS edition FROM requests r JOIN shifts s ON s.id=r.shift_id JOIN participations p ON p.id=s.participation_id JOIN editions e ON e.id=p.edition_id WHERE r.worker_id=? ORDER BY s.starts_at,r.id", t.worker).Scan(&v.OwnRequests).Error; err != nil {
				return err
			}
			for i := range v.OwnRequests {
				r := &v.OwnRequests[i]
				sh, p, err := t.shift(r.ShiftID, false)
				if err != nil {
					return err
				}
				r.Time = sh.Time()
				r.Stale = sh.Version != r.ShiftVersion
				r.CanWithdraw = r.Status == "pending" && p.Enabled && p.EditionID == t.current
			}
		}
		if v.Selected == "" {
			return nil
		}
		edition, err := t.edition(v.Selected)
		if err != nil {
			return err
		}
		var ps []Participation
		if err = t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.edition_id=? ORDER BY s.name,p.id", v.Selected).Scan(&ps).Error; err != nil {
			return err
		}
		for _, p := range ps {
			if err = t.permitPart(p, "view_schedule"); errors.Is(err, ErrDenied) {
				continue
			} else if err != nil {
				return err
			}
			schedule := Schedule{Participation: p, Edition: edition}
			err = t.permitPart(p, "edit_schedule")
			schedule.CanEdit = err == nil
			if err != nil && !errors.Is(err, ErrDenied) {
				return err
			}
			err = t.memberPermission(p.StandID)
			schedule.CanManageMembers = err == nil
			if err != nil && !errors.Is(err, ErrDenied) {
				return err
			}
			var shifts []Shift
			if err = t.db.Raw("SELECT * FROM shifts WHERE participation_id=? ORDER BY starts_at,id", p.ID).Scan(&shifts).Error; err != nil {
				return err
			}
			for _, sh := range shifts {
				sv := ShiftView{Shift: sh, Outside: !sh.Within(edition)}
				if err = t.db.Raw("SELECT w.id,w.first_name||' '||w.last_name AS name FROM assignments a JOIN workers w ON w.id=a.worker_id WHERE a.shift_id=? AND a.active ORDER BY w.last_name,w.first_name,w.id", sh.ID).Scan(&sv.Roster).Error; err != nil {
					return err
				}
				sv.Free = sh.Capacity - len(sv.Roster)
				if schedule.CanEdit || t.global {
					if err = t.db.Raw("SELECT r.*,w.first_name||' '||w.last_name AS name FROM requests r JOIN workers w ON w.id=r.worker_id WHERE r.shift_id=? AND r.status='pending' ORDER BY r.created_at,r.id", sh.ID).Scan(&sv.Pending).Error; err != nil {
						return err
					}
					for i := range sv.Pending {
						sv.Pending[i].Stale = sv.Pending[i].ShiftVersion != sh.Version
					}
					// Do not expose other-stand details; a nonblocking overlap marker is enough.
					if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM assignments a JOIN assignments b ON b.worker_id=a.worker_id JOIN shifts other ON other.id=b.shift_id WHERE a.shift_id=? AND a.active AND b.active AND b.shift_id<>? AND NOT other.cancelled AND other.starts_at<? AND other.ends_at>?)", sh.ID, sh.ID, sh.EndsAt, sh.StartsAt).Scan(&sv.Overlap).Error; err != nil {
						return err
					}
				}
				schedule.Shifts = append(schedule.Shifts, sv)
			}
			if schedule.CanManageMembers {
				if err = t.db.Raw("SELECT w.id,w.first_name||' '||w.last_name AS name FROM workers w JOIN memberships m ON m.worker_id=w.id WHERE m.stand_id=? ORDER BY w.last_name,w.first_name,w.id", p.StandID).Scan(&schedule.Members).Error; err != nil {
					return err
				}
			}
			if t.global {
				if err = t.db.Raw("SELECT a.id,a.subject AS name FROM teams tm JOIN accounts a ON a.id=tm.account_id WHERE tm.participation_id=? ORDER BY a.id", p.ID).Scan(&schedule.Team).Error; err != nil {
					return err
				}
			}
			v.Schedules = append(v.Schedules, schedule)
		}
		if t.global {
			if err = t.admin("view_feedback"); err != nil {
				return err
			}
			if err = t.db.Raw("SELECT id,title,created_at FROM feedback_notes WHERE edition_id=? ORDER BY created_at DESC,id DESC", v.Selected).Scan(&v.Notes).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return v, err
}
func (s *Store) AddFeedback(ctx context.Context, who Identity, edition, title string) error {
	return s.transact(ctx, who, false, func(t *Tx) error {
		if err := t.admin("create_feedback"); err != nil {
			return err
		}
		if edition == "" {
			edition = t.current
		}
		if _, err := t.edition(edition); err != nil {
			return err
		}
		if !textValid(title, 200) {
			return ErrInvalid
		}
		return t.db.Exec("INSERT INTO feedback_notes(title,created_at,edition_id) VALUES(?,?,?)", clean(title), t.store.Now(), edition).Error
	})
}

type DirectoryResult struct {
	Worker
	Full bool
}

func (s *Store) Directory(ctx context.Context, who Identity, query string) ([]DirectoryResult, error) {
	var out []DirectoryResult
	err := s.transact(ctx, who, false, func(t *Tx) error {
		if !t.global {
			var ps []Participation
			if err := t.db.Raw("SELECT p.* FROM participations p JOIN teams t ON t.participation_id=p.id WHERE t.account_id=? AND p.edition_id=? AND p.enabled", t.account, t.current).Scan(&ps).Error; err != nil {
				return err
			}
			allowed := false
			for _, p := range ps {
				err := t.permitPart(p, "edit_schedule")
				if err == nil {
					allowed = true
					break
				}
				if !errors.Is(err, ErrDenied) {
					return err
				}
			}
			if !allowed {
				return ErrDenied
			}
		} else {
			if err := t.admin("manage_catalog"); err != nil {
				return err
			}
		}
		query = clean(query)
		if len(query) < 2 || len(query) > 200 {
			return nil
		}
		var workers []Worker
		// Contact lookup is exact; name matching is literal, not user SQL wildcards.
		if err := t.db.Raw("SELECT * FROM workers WHERE position(lower(?) in lower(first_name||' '||last_name))>0 OR lower(email)=lower(?) OR phone=? ORDER BY last_name,first_name,id LIMIT 50", query, query, query).Scan(&workers).Error; err != nil {
			return err
		}
		for _, w := range workers {
			err := t.permitWorker(w.ID, "view_details")
			full := err == nil
			if err != nil && !errors.Is(err, ErrDenied) {
				return err
			}
			if !full {
				w.Email = mask(w.Email)
				w.Phone = mask(w.Phone)
				w.Notes = ""
				w.AccountID = nil
			}
			out = append(out, DirectoryResult{Worker: w, Full: full})
		}
		return nil
	})
	return out, err
}
func mask(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) < 3 {
		return "•••"
	}
	return string(r[:1]) + "•••" + string(r[len(r)-1:])
}
func (s *Store) Worker(ctx context.Context, who Identity, id string) (Worker, error) {
	var w Worker
	err := s.transact(ctx, who, false, func(t *Tx) error {
		if err := t.permitWorker(id, "view_details"); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT * FROM workers WHERE id=?", id).Scan(&w).Error; err != nil {
			return err
		}
		if w.ID == "" {
			return ErrInvalid
		}
		return nil
	})
	return w, err
}

type SignupView struct {
	Name, Edition string
	Shifts        []ShiftView
}

func (s *Store) Signup(ctx context.Context, token string) (SignupView, error) {
	var v SignupView
	err := s.transact(ctx, Identity{}, false, func(t *Tx) error {
		c, err := t.capability(token)
		if err != nil {
			return err
		}
		if c.Kind != "signup" || c.ParticipationID == nil {
			return ErrDenied
		}
		p, err := t.participation(*c.ParticipationID)
		if err != nil {
			return err
		}
		e, err := t.edition(p.EditionID)
		if err != nil {
			return err
		}
		v.Name = p.Name
		v.Edition = e.Label
		var shifts []Shift
		if err = t.db.Raw("SELECT * FROM shifts WHERE participation_id=? ORDER BY starts_at,id", p.ID).Scan(&shifts).Error; err != nil {
			return err
		}
		for _, sh := range shifts {
			if c.ShiftID != nil && *c.ShiftID != sh.ID {
				continue
			}
			if err = t.signupPermit(sh, p, "view_signup"); err != nil {
				return err
			}
			var assigned int64
			if err = t.db.Raw("SELECT count(*) FROM assignments WHERE shift_id=? AND active", sh.ID).Scan(&assigned).Error; err != nil {
				return err
			}
			v.Shifts = append(v.Shifts, ShiftView{Shift: sh, Free: sh.Capacity - int(assigned)})
		}
		// Even an empty schedule must enforce capability eligibility and engine uptime.
		if len(v.Shifts) == 0 {
			if p.EditionID != t.current || !p.Enabled || !s.Now().Before(day(e.EndDay).AddDate(0, 0, 1)) {
				return ErrDenied
			}
			t.resetFacts()
			t.fact(t.principal(), "owner", "worker:availability")
			return t.check("view_details", "worker:availability")
		}
		return nil
	})
	return v, err
}

// InspectProof never consumes a bearer proof and exposes only its kind. Identity
// binding and issuer-grant validity are rechecked when the POST is committed.
func (s *Store) InspectProof(ctx context.Context, token string) (string, error) {
	var kind string
	err := s.transact(ctx, Identity{}, false, func(t *Tx) error {
		c, err := t.capability(token)
		if err != nil {
			return err
		}
		if c.Kind == "signup" || c.ExpiresAt == nil || !s.Now().Before(*c.ExpiresAt) || c.ConsumedBy != nil {
			return ErrDenied
		}
		t.fact(t.principal(), "owner", "worker:proof")
		if err = t.check("view_details", "worker:proof"); err != nil {
			return err
		}
		kind = c.Kind
		return nil
	})
	return strings.TrimSpace(kind), err
}
