package scheduling

import (
	"context"
	"errors"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type EditionRow struct {
	Edition
	Participations, Enabled, MissingTeams int
}
type StandRow struct {
	ID, Name string
	Members  int
}
type ParticipationRow struct {
	Participation
	TeamCount, ShiftCount int
}
type OrganisationPage struct {
	Shell                                                        ui.ShellModel
	Editions                                                     []EditionRow
	Stands                                                       []StandRow
	Participations                                               []ParticipationRow
	Edition                                                      Edition
	Stand                                                        StandRow
	Participation                                                Participation
	Team, Accounts, Globals                                      []Person
	Links                                                        []CapabilityView
	EditionCount, StandCount, GlobalCount, Enabled, MissingTeams int
	Ready, HasNext                                               bool
	Page                                                         int
	Query                                                        string
}

func (t *Tx) organisationPermission() error { return t.admin("manage_catalog") }
func (s *Store) OrganisationOverview(ctx context.Context, who Identity, edition string) (OrganisationPage, error) {
	var v OrganisationPage
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.organisationPermission(); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT count(*) FROM editions").Scan(&v.EditionCount).Error; err != nil {
			return err
		}
		if err := t.db.Raw("SELECT count(*) FROM stands").Scan(&v.StandCount).Error; err != nil {
			return err
		}
		if err := t.db.Raw("SELECT count(*) FROM global_grants").Scan(&v.GlobalCount).Error; err != nil {
			return err
		}
		if shell.Selected != "" {
			return t.loadEditionAdmin(shell.Selected, &v)
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) EditionList(ctx context.Context, who Identity, edition string, page int) (OrganisationPage, error) {
	v := OrganisationPage{Page: max(page, 1)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.organisationPermission(); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT e.*,count(p.id) AS participations,count(p.id) FILTER(WHERE p.enabled) AS enabled,count(p.id) FILTER(WHERE p.enabled AND NOT EXISTS(SELECT 1 FROM teams tm WHERE tm.participation_id=p.id)) AS missing_teams FROM editions e LEFT JOIN participations p ON p.edition_id=e.id GROUP BY e.id ORDER BY e.start_day DESC,e.label,e.id LIMIT 26 OFFSET ?", (v.Page-1)*25).Scan(&v.Editions).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Editions) > 25
		if v.HasNext {
			v.Editions = v.Editions[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (t *Tx) loadEditionAdmin(id string, v *OrganisationPage) error {
	e, err := t.edition(id)
	if err != nil {
		return err
	}
	v.Edition = e
	if err = t.db.Raw("SELECT p.*,s.name,(SELECT count(*) FROM teams tm WHERE tm.participation_id=p.id) AS team_count,(SELECT count(*) FROM shifts sh WHERE sh.participation_id=p.id AND NOT sh.cancelled) AS shift_count FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.edition_id=? ORDER BY s.name,p.id LIMIT 100", id).Scan(&v.Participations).Error; err != nil {
		return err
	}
	if err = t.db.Raw("SELECT count(*) FROM participations WHERE edition_id=? AND enabled", id).Scan(&v.Enabled).Error; err != nil {
		return err
	}
	if err = t.db.Raw("SELECT count(*) FROM participations p WHERE p.edition_id=? AND p.enabled AND NOT EXISTS(SELECT 1 FROM teams tm WHERE tm.participation_id=p.id)", id).Scan(&v.MissingTeams).Error; err != nil {
		return err
	}
	err = t.ready(id)
	v.Ready = err == nil
	if err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	return nil
}
func (s *Store) EditionAdmin(ctx context.Context, who Identity, edition, id string) (OrganisationPage, error) {
	var v OrganisationPage
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.organisationPermission(); err != nil {
			return err
		}
		if id != "" {
			if err := t.loadEditionAdmin(id, &v); err != nil {
				return err
			}
		}
		return t.db.Raw("SELECT s.id,s.name,(SELECT count(*) FROM memberships m WHERE m.stand_id=s.id) AS members FROM stands s WHERE NOT EXISTS(SELECT 1 FROM participations p WHERE p.stand_id=s.id AND p.edition_id=?) ORDER BY s.name,s.id LIMIT 100", id).Scan(&v.Stands).Error
	})
	v.Shell = shell
	return v, err
}
func (s *Store) StandList(ctx context.Context, who Identity, edition string, page int) (OrganisationPage, error) {
	v := OrganisationPage{Page: max(page, 1)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.organisationPermission(); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT s.id,s.name,(SELECT count(*) FROM memberships m WHERE m.stand_id=s.id) AS members FROM stands s ORDER BY s.name,s.id LIMIT 26 OFFSET ?", (v.Page-1)*25).Scan(&v.Stands).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Stands) > 25
		if v.HasNext {
			v.Stands = v.Stands[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) StandAdmin(ctx context.Context, who Identity, edition, id string) (OrganisationPage, error) {
	var v OrganisationPage
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.organisationPermission(); err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		if err := t.db.Raw("SELECT s.id,s.name,(SELECT count(*) FROM memberships m WHERE m.stand_id=s.id) AS members FROM stands s WHERE s.id=?", id).Scan(&v.Stand).Error; err != nil {
			return err
		}
		if v.Stand.ID == "" {
			return ErrNotFound
		}
		return t.db.Raw("SELECT p.*,e.label AS name,(SELECT count(*) FROM teams tm WHERE tm.participation_id=p.id) AS team_count FROM participations p JOIN editions e ON e.id=p.edition_id WHERE p.stand_id=? ORDER BY e.start_day DESC,p.id LIMIT 50", id).Scan(&v.Participations).Error
	})
	v.Shell = shell
	return v, err
}
func (s *Store) ParticipationAdmin(ctx context.Context, who Identity, edition, parent, id, query string) (OrganisationPage, error) {
	v := OrganisationPage{Query: clean(query)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.admin("manage_teams"); err != nil {
			return err
		}
		p, err := t.participation(id)
		if err != nil {
			return err
		}
		if p.EditionID != parent {
			return ErrDenied
		}
		v.Participation = p
		v.Edition, err = t.edition(parent)
		if err != nil {
			return err
		}
		if err = t.db.Raw("SELECT a.id,COALESCE(NULLIF(w.first_name||' '||w.last_name,''),a.subject) AS name FROM accounts a JOIN teams tm ON tm.account_id=a.id LEFT JOIN workers w ON w.account_id=a.id WHERE tm.participation_id=? ORDER BY a.id LIMIT 100", id).Scan(&v.Team).Error; err != nil {
			return err
		}
		q := v.Query
		if len(q) > 200 {
			return ErrInvalid
		}
		if err = t.db.Raw("SELECT a.id,COALESCE(NULLIF(w.first_name||' '||w.last_name,''),a.subject) AS name FROM accounts a LEFT JOIN workers w ON w.account_id=a.id WHERE (a.id=? OR position(lower(?) in lower(COALESCE(w.first_name||' '||w.last_name,a.subject)))>0) AND NOT EXISTS(SELECT 1 FROM teams tm WHERE tm.participation_id=? AND tm.account_id=a.id) ORDER BY a.id LIMIT 25", q, q, id).Scan(&v.Accounts).Error; err != nil {
			return err
		}
		var caps []Capability
		if err = t.db.Raw("SELECT * FROM capabilities WHERE participation_id=? ORDER BY created_at DESC,digest LIMIT 10", id).Scan(&caps).Error; err != nil {
			return err
		}
		for _, c := range caps {
			if err = t.capabilityPermission(c); err != nil {
				return err
			}
			cv, err := t.capabilityView(c)
			if err != nil {
				return err
			}
			v.Links = append(v.Links, cv)
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) AccessAdmin(ctx context.Context, who Identity, edition string, page int) (OrganisationPage, error) {
	v := OrganisationPage{Page: max(page, 1)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.admin("manage_global_grants"); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT a.id,COALESCE(NULLIF(w.first_name||' '||w.last_name,''),a.subject) AS name FROM accounts a JOIN global_grants g ON g.account_id=a.id LEFT JOIN workers w ON w.account_id=a.id ORDER BY a.id LIMIT 26 OFFSET ?", (v.Page-1)*25).Scan(&v.Globals).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Globals) > 25
		if v.HasNext {
			v.Globals = v.Globals[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
