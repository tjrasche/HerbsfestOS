package scheduling

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type Store struct {
	DB  *gorm.DB
	FGA Checker
	Now func() time.Time
}

func New(db *gorm.DB, fga Checker) *Store { return &Store{DB: db, FGA: fga, Now: time.Now} }

type Tx struct {
	db                       *gorm.DB
	store                    *Store
	account, worker, current string
	global                   bool
	exclusive                bool
	facts                    []Tuple
}

// All reads and writes take the revision lock before consulting trusted facts.
// READ COMMITTED sees revocations committed before the lock was obtained.
// Exclusive actions can change memberships, identity bindings, or context gates.
func (s *Store) transact(ctx context.Context, who Identity, exclusive bool, fn func(*Tx) error) error {
	return s.DB.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		lock := "FOR SHARE"
		if exclusive {
			lock = "FOR UPDATE"
		}
		var state struct {
			CurrentEdition *string
			Revision       int64
		}
		if err := db.Raw("SELECT current_edition,revision FROM scheduling_state WHERE id=1 " + lock).Scan(&state).Error; err != nil {
			return err
		}
		t := &Tx{db: db, store: s, exclusive: exclusive}
		if state.CurrentEdition != nil {
			t.current = *state.CurrentEdition
		}
		if who.Issuer != "" && who.Subject != "" {
			if err := db.Raw("SELECT id FROM accounts WHERE issuer=? AND subject=?", who.Issuer, who.Subject).Scan(&t.account).Error; err != nil {
				return err
			}
			if t.account != "" {
				if err := db.Raw("SELECT id FROM workers WHERE account_id=?", t.account).Scan(&t.worker).Error; err != nil {
					return err
				}
				if err := db.Raw("SELECT EXISTS(SELECT 1 FROM global_grants WHERE account_id=?)", t.account).Scan(&t.global).Error; err != nil {
					return err
				}
			}
		}
		t.resetFacts()
		if err := fn(t); err != nil {
			return err
		}
		if exclusive {
			return db.Exec("UPDATE scheduling_state SET revision=revision+1 WHERE id=1").Error
		}
		return nil
	})
}
func (t *Tx) principal() string {
	if t.account == "" {
		return "principal:guest"
	}
	return "principal:" + t.account
}
func (t *Tx) fact(user, relation, object string) {
	t.facts = append(t.facts, Tuple{user, relation, object})
}
func (t *Tx) resetFacts() {
	t.facts = nil
	if t.global {
		t.fact(t.principal(), "global", "festival:herbstfest")
	}
}
func (t *Tx) check(permission, object string) error {
	if len(t.facts) > 100 || t.store.FGA == nil {
		return ErrUnavailable
	}
	allowed, err := t.store.FGA.CheckContext(t.db.Statement.Context, t.principal(), permission, object, t.facts)
	if err != nil {
		return ErrUnavailable
	}
	if !allowed {
		return ErrDenied
	}
	return nil
}
func (t *Tx) admin(permission string) error {
	t.resetFacts()
	return t.check(permission, "festival:herbstfest")
}
func (t *Tx) participation(id string) (Participation, error) {
	var p Participation
	err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.id=?", id).Scan(&p).Error
	if err != nil {
		return p, err
	}
	if p.ID == "" {
		return p, ErrInvalid
	}
	return p, nil
}
func (t *Tx) edition(id string) (Edition, error) {
	var e Edition
	err := t.db.Raw("SELECT * FROM editions WHERE id=?", id).Scan(&e).Error
	if err == nil && e.ID == "" {
		err = ErrInvalid
	}
	return e, err
}
func (t *Tx) partFacts(p Participation) error {
	po, so := "participation:"+p.ID, "stand:"+p.StandID
	t.fact("festival:herbstfest", "festival", po)
	t.fact(so, "stand", po)
	t.fact("festival:herbstfest", "festival", so)
	t.fact(po, "participation", so)
	if p.EditionID == t.current {
		t.fact("principal:*", "current", po)
	}
	if p.Enabled {
		t.fact("principal:*", "enabled", po)
	}
	var team, member bool
	if err := t.db.Raw("SELECT EXISTS(SELECT 1 FROM teams WHERE participation_id=? AND account_id=?)", p.ID, t.account).Scan(&team).Error; err != nil {
		return err
	}
	if err := t.db.Raw("SELECT EXISTS(SELECT 1 FROM memberships WHERE stand_id=? AND worker_id=?)", p.StandID, t.worker).Scan(&member).Error; err != nil {
		return err
	}
	if team {
		t.fact(t.principal(), "team", po)
	}
	if member {
		t.fact(t.principal(), "member", so)
	}
	return nil
}
func (t *Tx) permitPart(p Participation, permission string) error {
	t.resetFacts()
	if err := t.partFacts(p); err != nil {
		return err
	}
	return t.check(permission, "participation:"+p.ID)
}
func (t *Tx) shift(id string, lock bool) (Shift, Participation, error) {
	var s Shift
	q := "SELECT * FROM shifts WHERE id=?"
	if lock {
		q += " FOR UPDATE"
	}
	if err := t.db.Raw(q, id).Scan(&s).Error; err != nil {
		return s, Participation{}, err
	}
	p, err := t.participation(s.ParticipationID)
	return s, p, err
}
func (t *Tx) workerFacts(id string) error {
	wo := "worker:" + id
	t.fact("festival:herbstfest", "festival", wo)
	if id == t.worker && id != "" {
		t.fact(t.principal(), "owner", wo)
	}
	// Only the actor's current teams which are relevant to this worker. A worker
	// may span arbitrarily many stands; each path is independent and checked alone.
	return nil
}
func (t *Tx) permitWorker(id, permission string) error {
	t.resetFacts()
	t.workerFacts(id)
	err := t.check(permission, "worker:"+id)
	if err == nil || !errors.Is(err, ErrDenied) {
		return err
	}
	var ps []Participation
	if err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id JOIN memberships m ON m.stand_id=p.stand_id JOIN teams t ON t.participation_id=p.id WHERE p.edition_id=? AND p.enabled AND t.account_id=? AND m.worker_id=? ORDER BY p.id", t.current, t.account, id).Scan(&ps).Error; err != nil {
		return err
	}
	for _, p := range ps {
		t.resetFacts()
		t.workerFacts(id)
		t.fact("stand:"+p.StandID, "member_stand", "worker:"+id)
		if err = t.partFacts(p); err != nil {
			return err
		}
		err = t.check(permission, "worker:"+id)
		if err == nil || !errors.Is(err, ErrDenied) {
			return err
		}
	}
	return ErrDenied
}
func (t *Tx) audit(action, resource string, details any) error {
	return t.db.Exec("INSERT INTO scheduling_audit(actor,action,resource,details) VALUES(?,?,?,?::jsonb)", t.principal(), action, resource, details).Error
}
func (t *Tx) ready(id string) error {
	e, err := t.edition(id)
	if err != nil {
		return err
	}
	if e.EndDay.Before(e.StartDay) {
		return ErrInvalid
	}
	var count, missing int64
	if err = t.db.Raw("SELECT count(*) FROM participations WHERE edition_id=? AND enabled", id).Scan(&count).Error; err != nil {
		return err
	}
	if err = t.db.Raw("SELECT count(*) FROM participations p WHERE p.edition_id=? AND p.enabled AND NOT EXISTS(SELECT 1 FROM teams t WHERE t.participation_id=p.id)", id).Scan(&missing).Error; err != nil {
		return err
	}
	if count == 0 || missing > 0 {
		return fmt.Errorf("%w Mindestens ein aktiver Stand und Teams für alle aktiven Stände erforderlich.", ErrConflict)
	}
	return nil
}
func (t *Tx) maintainReady() error {
	if t.current == "" {
		return nil
	}
	return t.ready(t.current)
}
func (t *Tx) ensureAccount(who Identity) (string, error) {
	if who.Issuer == "" || who.Subject == "" {
		return "", ErrDenied
	}
	id := ID()
	if err := t.db.Exec("INSERT INTO accounts(id,issuer,subject) VALUES(?,?,?) ON CONFLICT(issuer,subject) DO NOTHING", id, who.Issuer, who.Subject).Error; err != nil {
		return "", err
	}
	var account string
	err := t.db.Raw("SELECT id FROM accounts WHERE issuer=? AND subject=?", who.Issuer, who.Subject).Scan(&account).Error
	return account, err
}
func clean(s string) string { return strings.TrimSpace(s) }
