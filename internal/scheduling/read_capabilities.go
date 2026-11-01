package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type ProofInfo struct {
	Kind, Scope, Purpose, Stand, Edition, WorkerName, AccountID string
	ExpiresAt                                                   time.Time
	SignedIn, CanProceed                                        bool
}

// ProofContext exposes only information justified by the bearer proof. It does
// not consume the proof or infer ownership from a contact address.
func (s *Store) ProofContext(ctx context.Context, token string, who Identity) (ProofInfo, error) {
	var v ProofInfo
	err := s.transact(ctx, who, false, func(t *Tx) error {
		c, err := t.capability(token)
		if err != nil {
			return err
		}
		if c.Kind == "signup" || c.ExpiresAt == nil || !s.Now().Before(*c.ExpiresAt) || c.ConsumedBy != nil {
			return ErrDenied
		}
		t.resetFacts()
		t.fact(t.principal(), "owner", "worker:proof")
		if err = t.check("view_details", "worker:proof"); err != nil {
			return err
		}
		v = ProofInfo{Kind: c.Kind, Scope: c.Scope, ExpiresAt: *c.ExpiresAt, AccountID: t.account, SignedIn: who.Issuer != "" && who.Subject != ""}
		v.CanProceed = v.SignedIn
		if c.Kind == "organizer" {
			if c.IssuerID == nil {
				return ErrDenied
			}
			var valid bool
			if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM global_grants WHERE account_id=?)", *c.IssuerID).Scan(&valid).Error; err != nil {
				return err
			}
			if !valid {
				return ErrDenied
			}
			if c.Scope == "global" {
				v.Purpose = "Zugriff auf die Gesamtorganisation"
			} else if c.Scope == "team" && c.ParticipationID != nil {
				p, err := t.participation(*c.ParticipationID)
				if err != nil {
					return err
				}
				e, err := t.edition(p.EditionID)
				if err != nil {
					return err
				}
				v.Stand = p.Name
				v.Edition = e.Label
				v.Purpose = "Standteam für " + p.Name
			} else {
				return ErrDenied
			}
		} else {
			if c.WorkerID == nil {
				return ErrDenied
			}
			var w Worker
			if err = t.db.Raw("SELECT * FROM workers WHERE id=?", *c.WorkerID).Scan(&w).Error; err != nil {
				return err
			}
			if w.ID == "" {
				return ErrDenied
			}
			v.WorkerName = w.Name()
			v.Purpose = "Helferdatensatz mit deinem Konto verbinden"
			if w.AccountID != nil && *w.AccountID != t.account {
				v.CanProceed = false
			}
			if t.worker != "" && t.worker != w.ID && c.Kind != "fresh" {
				v.CanProceed = false
			}
		}
		return nil
	})
	return v, err
}

type CapabilityView struct {
	ID, Kind, Scope, Label, Status, Issuer, ConsumedAccount string
	CreatedAt                                               time.Time
	ExpiresAt                                               *time.Time
	CanReissue, CanRevoke                                   bool
}
type CapabilitiesPage struct {
	Shell        ui.ShellModel
	Rows         []CapabilityView
	Page         int
	HasNext      bool
	Kind, Status string
}
type CapabilityRecordPage struct {
	Shell ui.ShellModel
	Link  CapabilityView
}

func (t *Tx) capabilityPermission(c Capability) error {
	switch c.Kind {
	case "organizer":
		return t.admin("issue_organizer_invite")
	case "connection", "fresh":
		if c.WorkerID == nil {
			return ErrInvalid
		}
		return t.connectionPermission(*c.WorkerID)
	case "signup":
		if c.ParticipationID == nil {
			return ErrInvalid
		}
		p, err := t.participation(*c.ParticipationID)
		if err != nil {
			return err
		}
		if t.global {
			return t.admin("manage_catalog")
		}
		return t.permitPart(p, "edit_schedule")
	}
	return ErrInvalid
}
func (t *Tx) managedCapability(id string) (Capability, error) {
	var c Capability
	if len(id) != 64 {
		return c, ErrDenied
	}
	if err := t.db.Raw("SELECT * FROM capabilities WHERE digest=?", id).Scan(&c).Error; err != nil {
		return c, err
	}
	if c.Digest == "" {
		return c, ErrDenied
	}
	return c, t.capabilityPermission(c)
}
func (t *Tx) capabilityView(c Capability) (CapabilityView, error) {
	v := CapabilityView{ID: c.Digest, Kind: c.Kind, Scope: c.Scope, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt, Status: "Aktiv", CanReissue: c.ConsumedBy == nil, CanRevoke: !c.Disabled}
	if c.IssuerID != nil {
		v.Issuer = shortID(*c.IssuerID)
	}
	if c.ConsumedBy != nil {
		v.ConsumedAccount = shortID(*c.ConsumedBy)
		v.Status = "Eingelöst"
		v.CanRevoke = false
	} else if c.Disabled {
		v.Status = "Widerrufen"
	} else if c.ExpiresAt != nil && !t.store.Now().Before(*c.ExpiresAt) {
		v.Status = "Abgelaufen"
	}
	if c.ParticipationID != nil {
		p, err := t.participation(*c.ParticipationID)
		if err != nil {
			return v, err
		}
		e, err := t.edition(p.EditionID)
		if err != nil {
			return v, err
		}
		v.Label = p.Name + " · " + e.Label
		if (c.Kind == "signup" || c.Kind == "organizer" && c.Scope == "team") && v.Status == "Aktiv" && (!p.Enabled || p.EditionID != t.current || c.Kind == "signup" && !t.store.Now().Before(day(e.EndDay).AddDate(0, 0, 1))) {
			v.Status = "Derzeit nicht nutzbar"
		}
		if c.ShiftID != nil {
			sh, _, err := t.shift(*c.ShiftID, false)
			if err != nil {
				return v, err
			}
			v.Label += " · " + sh.Title
			if sh.Cancelled && v.Status == "Aktiv" {
				v.Status = "Derzeit nicht nutzbar"
			}
		}
	} else if c.Kind == "organizer" {
		v.Label = "Gesamtorganisation"
	}
	if c.WorkerID != nil {
		var w Worker
		if err := t.db.Raw("SELECT * FROM workers WHERE id=?", *c.WorkerID).Scan(&w).Error; err != nil {
			return v, err
		}
		v.Label = w.Name()
		if w.AccountID != nil {
			v.CanReissue = false
			if v.Status == "Aktiv" {
				v.Status = "Derzeit nicht nutzbar"
			}
		}
	}
	if c.Kind == "organizer" && c.IssuerID != nil && v.Status == "Aktiv" {
		var ok bool
		if err := t.db.Raw("SELECT EXISTS(SELECT 1 FROM global_grants WHERE account_id=?)", *c.IssuerID).Scan(&ok).Error; err != nil {
			return v, err
		}
		if !ok {
			v.Status = "Derzeit nicht nutzbar"
		}
	}
	return v, nil
}
func (s *Store) CapabilityDetail(ctx context.Context, who Identity, id, edition string) (CapabilityRecordPage, error) {
	var v CapabilityRecordPage
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		c, err := t.managedCapability(id)
		if err != nil {
			return err
		}
		v.Link, err = t.capabilityView(c)
		return err
	})
	v.Shell = shell
	return v, err
}
func (s *Store) Capabilities(ctx context.Context, who Identity, edition, kind, status string, page int) (CapabilitiesPage, error) {
	v := CapabilitiesPage{Page: max(1, page), Kind: kind, Status: status}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.admin("issue_organizer_invite"); err != nil {
			return err
		}
		// The global invitation list intentionally lists organiser proofs only.
		var rows []Capability
		query := t.db.Table("capabilities").Where("kind = ?", "organizer")
		switch status {
		case "active":
			query = query.Where("NOT disabled AND consumed_by IS NULL AND expires_at > ?", s.Now())
		case "revoked":
			query = query.Where("disabled AND consumed_by IS NULL")
		case "consumed":
			query = query.Where("consumed_by IS NOT NULL")
		case "expired":
			query = query.Where("NOT disabled AND consumed_by IS NULL AND expires_at <= ?", s.Now())
		case "":
		default:
			return ErrInvalid
		}
		if kind != "" && kind != "global" && kind != "team" {
			return ErrInvalid
		}
		if kind != "" {
			query = query.Where("scope=?", kind)
		}
		if err := query.Order("created_at DESC,digest DESC").Offset((v.Page - 1) * 25).Limit(26).Find(&rows).Error; err != nil {
			return err
		}
		v.HasNext = len(rows) > 25
		if v.HasNext {
			rows = rows[:25]
		}
		for _, c := range rows {
			if err := t.capabilityPermission(c); err != nil {
				return err
			}
			row, err := t.capabilityView(c)
			if err != nil {
				return err
			}
			v.Rows = append(v.Rows, row)
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func allowed(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrDenied) {
		return false, nil
	}
	return false, err
}
