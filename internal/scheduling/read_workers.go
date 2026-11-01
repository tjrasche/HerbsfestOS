package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type DirectoryQuery struct {
	Edition, Query, Stand, Day, Shift, ReturnTo, MemberStand string
	Page                                                     int
}
type DirectoryPageModel struct {
	Shell                                       ui.ShellModel
	Rows                                        []DirectoryResult
	MemberStand                                 Person
	AlreadyMembers                              map[string]bool
	Query, Stand, StandID, Day, Shift, ReturnTo string
	Page                                        int
	HasNext                                     bool
}
type WorkerProfile struct {
	Shell                                             ui.ShellModel
	Worker                                            Worker
	CanEditContacts, CanEditNotes, CanIssueConnection bool
	Memberships                                       []Person
	Assignments                                       []WorkerAssignment
	Links                                             []CapabilityView
}
type WorkerAssignment struct {
	ShiftID, Title, Stand, Edition string
	StartsAt, EndsAt               time.Time
	Cancelled                      bool
}
type AssignmentChoice struct {
	Shift
	Stand     string
	Confirmed int
}
type AssignmentPage struct {
	Shell                              ui.ShellModel
	Worker                             Person
	Choices                            []AssignmentChoice
	Stands                             []Person
	MembershipStands                   []Person
	Stand, Day, Query, Shift, ReturnTo string
	Page                               int
	HasNext                            bool
}
type MembershipPage struct {
	Shell   ui.ShellModel
	Stand   Person
	Rows    []Person
	Query   string
	Page    int
	HasNext bool
}

func (t *Tx) directoryPermission() error {
	if t.global {
		return t.admin("manage_catalog")
	}
	var ps []Participation
	if err := t.db.Raw("SELECT p.* FROM participations p JOIN teams tm ON tm.participation_id=p.id WHERE tm.account_id=? AND p.edition_id=? AND p.enabled ORDER BY p.id", t.account, t.current).Scan(&ps).Error; err != nil {
		return err
	}
	for _, p := range ps {
		err := t.permitPart(p, "edit_schedule")
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrDenied) {
			return err
		}
	}
	return ErrDenied
}
func (s *Store) SearchWorkers(ctx context.Context, who Identity, q DirectoryQuery) (DirectoryPageModel, error) {
	v := DirectoryPageModel{Query: clean(q.Query), Stand: q.Stand, Day: q.Day, Shift: q.Shift, ReturnTo: q.ReturnTo, Page: max(1, q.Page)}
	shell, err := s.readPage(ctx, who, q.Edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.directoryPermission(); err != nil {
			return err
		}
		if q.MemberStand != "" {
			if err := t.memberPermission(q.MemberStand); err != nil {
				return err
			}
			if err := t.db.Raw("SELECT id,name FROM stands WHERE id=?", q.MemberStand).Scan(&v.MemberStand).Error; err != nil {
				return err
			}
			if v.MemberStand.ID == "" {
				return ErrDenied
			}
		}
		if v.Stand != "" {
			p, err := t.participation(v.Stand)
			if err != nil {
				return err
			}
			if err = t.permitPart(p, "edit_schedule"); err != nil {
				return err
			}
			if p.EditionID != shell.Selected {
				return ErrDenied
			}
			v.StandID = p.StandID
		}
		if v.Shift != "" {
			_, p, err := t.shift(v.Shift, false)
			if err != nil {
				return err
			}
			if v.Stand != "" && p.ID != v.Stand {
				return ErrDenied
			}
			if p.EditionID != shell.Selected {
				return ErrDenied
			}
			if err = t.permitPart(p, "edit_schedule"); err != nil {
				return err
			}
			v.Stand = p.ID
			v.StandID = p.StandID
		}
		if v.Day != "" && !validDay(v.Day) {
			return ErrInvalid
		}
		if len(v.Query) > 200 {
			return ErrInvalid
		}
		if len(v.Query) < 2 {
			return nil
		}
		var rows []Worker
		if err := t.db.Raw("SELECT * FROM workers WHERE position(lower(?) in lower(first_name||' '||last_name))>0 OR lower(email)=lower(?) OR phone=? ORDER BY last_name,first_name,id LIMIT 26 OFFSET ?", v.Query, v.Query, v.Query, (v.Page-1)*25).Scan(&rows).Error; err != nil {
			return err
		}
		v.HasNext = len(rows) > 25
		if v.HasNext {
			rows = rows[:25]
		}
		for _, w := range rows {
			full, err := allowed(t.permitWorker(w.ID, "view_details"))
			if err != nil {
				return err
			}
			if !full {
				w.Email = mask(w.Email)
				w.Phone = mask(w.Phone)
				w.Notes = ""
				w.AccountID = nil
			}
			v.Rows = append(v.Rows, DirectoryResult{Worker: w, Full: full})
		}
		if v.MemberStand.ID != "" && len(v.Rows) > 0 {
			ids := make([]string, 0, len(v.Rows))
			for _, row := range v.Rows {
				ids = append(ids, row.ID)
			}
			var memberIDs []string
			if err := t.db.Raw("SELECT worker_id FROM memberships WHERE stand_id=? AND worker_id IN ?", v.MemberStand.ID, ids).Scan(&memberIDs).Error; err != nil {
				return err
			}
			v.AlreadyMembers = make(map[string]bool, len(memberIDs))
			for _, id := range memberIDs {
				v.AlreadyMembers[id] = true
			}
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) WorkerDetail(ctx context.Context, who Identity, edition, id string) (WorkerProfile, error) {
	var v WorkerProfile
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.permitWorker(id, "view_details"); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT * FROM workers WHERE id=?", id).Scan(&v.Worker).Error; err != nil {
			return err
		}
		if v.Worker.ID == "" {
			return ErrNotFound
		}
		var err error
		v.CanEditContacts, err = allowed(t.permitWorker(id, "edit_contacts"))
		if err != nil {
			return err
		}
		v.CanEditNotes, err = allowed(t.permitWorker(id, "edit_notes"))
		if err != nil {
			return err
		}
		v.CanIssueConnection, err = allowed(t.connectionPermission(id))
		if err != nil {
			return err
		}
		v.CanIssueConnection = v.CanIssueConnection && v.Worker.AccountID == nil
		var members []Person
		if err = t.db.Raw("SELECT s.id,s.name FROM stands s JOIN memberships m ON m.stand_id=s.id WHERE m.worker_id=? ORDER BY s.name,s.id", id).Scan(&members).Error; err != nil {
			return err
		}
		for _, m := range members {
			if id == t.worker {
				v.Memberships = append(v.Memberships, m)
				continue
			}
			ok, err := allowed(t.memberPermission(m.ID))
			if err != nil {
				return err
			}
			if ok {
				v.Memberships = append(v.Memberships, m)
			}
		}
		var ps []Participation
		if shell.Selected != "" {
			if err = t.db.Raw("SELECT DISTINCT p.*,st.name FROM participations p JOIN stands st ON st.id=p.stand_id JOIN shifts sh ON sh.participation_id=p.id JOIN assignments a ON a.shift_id=sh.id WHERE a.worker_id=? AND a.active AND p.edition_id=? ORDER BY p.id", id, shell.Selected).Scan(&ps).Error; err != nil {
				return err
			}
		}
		for _, p := range ps {
			ok, err := allowed(t.permitPart(p, "view_schedule"))
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			var rows []WorkerAssignment
			if err = t.db.Raw("SELECT sh.id AS shift_id,sh.title,sh.starts_at,sh.ends_at,sh.cancelled,st.name AS stand,e.label AS edition FROM assignments a JOIN shifts sh ON sh.id=a.shift_id JOIN participations p ON p.id=sh.participation_id JOIN stands st ON st.id=p.stand_id JOIN editions e ON e.id=p.edition_id WHERE a.worker_id=? AND a.active AND p.id=? ORDER BY sh.starts_at,sh.id LIMIT 25", id, p.ID).Scan(&rows).Error; err != nil {
				return err
			}
			v.Assignments = append(v.Assignments, rows...)
			if len(v.Assignments) > 25 {
				v.Assignments = v.Assignments[:25]
				break
			}
		}
		if v.CanIssueConnection {
			var caps []Capability
			if err = t.db.Raw("SELECT * FROM capabilities WHERE worker_id=? AND kind IN ('connection','fresh') ORDER BY created_at DESC,digest LIMIT 5", id).Scan(&caps).Error; err != nil {
				return err
			}
			for _, c := range caps {
				cv, err := t.capabilityView(c)
				if err != nil {
					return err
				}
				v.Links = append(v.Links, cv)
			}
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) AssignmentChoices(ctx context.Context, who Identity, id string, filter ScheduleFilter, query string) (AssignmentPage, error) {
	v := AssignmentPage{Stand: filter.Stand, Day: filter.Day, Page: max(filter.Page, 1), Query: query}
	shell, err := s.readPage(ctx, who, filter.Edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.directoryPermission(); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT id,first_name||' '||last_name AS name FROM workers WHERE id=?", id).Scan(&v.Worker).Error; err != nil {
			return err
		}
		if v.Worker.ID == "" {
			return ErrDenied
		}
		var ps []Participation
		if shell.Selected != "" {
			if err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.edition_id=? ORDER BY s.name,p.id", shell.Selected).Scan(&ps).Error; err != nil {
				return err
			}
		}
		var chosen *Participation
		for _, p := range ps {
			member, memberErr := allowed(t.memberPermission(p.StandID))
			if memberErr != nil {
				return memberErr
			}
			if member {
				v.MembershipStands = append(v.MembershipStands, Person{p.StandID, p.Name})
			}
			ok, err := allowed(t.permitPart(p, "edit_schedule"))
			if err != nil {
				return err
			}
			if ok {
				v.Stands = append(v.Stands, Person{p.ID, p.Name})
				if p.ID == v.Stand || v.Stand == "" && chosen == nil {
					cp := p
					chosen = &cp
				}
			}
		}
		if chosen == nil {
			if v.Stand != "" {
				return ErrDenied
			}
			return nil
		}
		v.Stand = chosen.ID
		if v.Day == "" {
			var first time.Time
			if err := t.db.Raw("SELECT COALESCE(min(starts_at),?) FROM shifts WHERE participation_id=? AND NOT cancelled", s.Now(), v.Stand).Scan(&first).Error; err != nil {
				return err
			}
			v.Day = first.In(Berlin).Format("2006-01-02")
		}
		if !validDay(v.Day) {
			return ErrInvalid
		}
		start, err := time.ParseInLocation("2006-01-02", v.Day, Berlin)
		if err != nil {
			return ErrInvalid
		}
		if err = t.db.Raw("SELECT sh.*,st.name AS stand,(SELECT count(*) FROM assignments a WHERE a.shift_id=sh.id AND a.active) AS confirmed FROM shifts sh JOIN participations p ON p.id=sh.participation_id JOIN stands st ON st.id=p.stand_id WHERE sh.participation_id=? AND NOT sh.cancelled AND sh.starts_at<? AND sh.ends_at>? ORDER BY sh.starts_at,sh.id LIMIT 26 OFFSET ?", v.Stand, start.AddDate(0, 0, 1), start, (v.Page-1)*25).Scan(&v.Choices).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Choices) > 25
		if v.HasNext {
			v.Choices = v.Choices[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
func (s *Store) Memberships(ctx context.Context, who Identity, edition, stand, query string, page int) (MembershipPage, error) {
	v := MembershipPage{Page: max(page, 1), Query: clean(query)}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, _ ui.ShellModel) error {
		if err := t.memberPermission(stand); err != nil {
			return err
		}
		if err := t.db.Raw("SELECT id,name FROM stands WHERE id=?", stand).Scan(&v.Stand).Error; err != nil {
			return err
		}
		if v.Stand.ID == "" {
			return ErrNotFound
		}
		if len(v.Query) > 200 {
			return ErrInvalid
		}
		if err := t.db.Raw("SELECT w.id,w.first_name||' '||w.last_name AS name FROM workers w JOIN memberships m ON m.worker_id=w.id WHERE m.stand_id=? AND position(lower(?) in lower(w.first_name||' '||w.last_name))>0 ORDER BY w.last_name,w.first_name,w.id LIMIT 26 OFFSET ?", stand, v.Query, (v.Page-1)*25).Scan(&v.Rows).Error; err != nil {
			return err
		}
		v.HasNext = len(v.Rows) > 25
		if v.HasNext {
			v.Rows = v.Rows[:25]
		}
		return nil
	})
	v.Shell = shell
	return v, err
}
