package scheduling

import (
	"context"
	"errors"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/url"
	"strings"
	"time"
)

type RequestRow struct {
	Request
	Name, Title, Stand, Edition, EditionID, ParticipationID string
	StartsAt, EndsAt                                        time.Time
	CurrentVersion, Capacity, Confirmed                     int
	Enabled, CanEdit, CanWithdraw, Stale                    bool
}

func (r RequestRow) Time() string { return conciseTime(Shift{StartsAt: r.StartsAt, EndsAt: r.EndsAt}) }

type RequestsModel struct {
	Shell                     ui.ShellModel
	Rows                      []RequestRow
	Filter                    ScheduleFilter
	Status                    string
	HasNext                   bool
	Previous, Next, ReturnURL string
	Stands                    []StandChoice
	Days                      []DayChoice
}

func (s *Store) OwnRequests(ctx context.Context, who Identity, edition, statusFilter string, page int) (RequestsModel, error) {
	out := RequestsModel{Filter: ScheduleFilter{Page: max(1, page)}, Status: statusFilter}
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		if t.worker == "" {
			return nil
		}
		if err := t.permitWorker(t.worker, "view_details"); err != nil {
			return err
		}
		q := `SELECT r.*,s.title,s.starts_at,s.ends_at,s.version AS current_version,p.enabled,p.id AS participation_id,e.id AS edition_id,e.label AS edition,st.name AS stand FROM requests r JOIN shifts s ON s.id=r.shift_id JOIN participations p ON p.id=s.participation_id JOIN editions e ON e.id=p.edition_id JOIN stands st ON st.id=p.stand_id WHERE r.worker_id=?`
		args := []any{t.worker}
		if statusFilter != "" {
			switch statusFilter {
			case "pending", "approved", "declined", "withdrawn", "cancelled":
				q += " AND r.status=?"
				args = append(args, statusFilter)
			default:
				return ErrInvalid
			}
		}
		q += " ORDER BY (e.id=?) DESC,s.starts_at DESC,r.id LIMIT 21 OFFSET ?"
		args = append(args, t.current, (out.Filter.Page-1)*20)
		if err := t.db.Raw(q, args...).Scan(&out.Rows).Error; err != nil {
			return err
		}
		if len(out.Rows) == 0 && out.Filter.Page > 1 {
			page, err := t.remainingRequestPage(q, args, 2)
			if err != nil {
				return err
			}
			out.Filter.Page = page
			args[len(args)-1] = (page - 1) * 20
			if err = t.db.Raw(q, args...).Scan(&out.Rows).Error; err != nil {
				return err
			}
		}
		if len(out.Rows) > 20 {
			out.HasNext = true
			out.Rows = out.Rows[:20]
		}
		for i := range out.Rows {
			row := &out.Rows[i]
			row.Stale = row.ShiftVersion != row.CurrentVersion
			row.CanWithdraw = row.Status == "pending" && row.Enabled && row.EditionID == t.current
		}
		return nil
	})
	out.Shell = shell
	if shell.Global {
		out.Filter.Edition = shell.Selected
	}
	q := contextQuery(shell)
	if statusFilter != "" {
		q.Set("status", statusFilter)
	}
	out.ReturnURL = canonical("/requests/mine", q)
	if out.Filter.Page > 1 {
		q.Set("page", n(out.Filter.Page-1))
		out.Previous = canonical("/requests/mine", q)
	}
	if out.HasNext {
		q.Set("page", n(out.Filter.Page+1))
		out.Next = canonical("/requests/mine", q)
	}
	return out, err
}
func (s *Store) PendingRequests(ctx context.Context, who Identity, filter ScheduleFilter) (RequestsModel, error) {
	out := RequestsModel{Filter: filter}
	if out.Filter.Page < 1 {
		out.Filter.Page = 1
	}
	shell, err := s.readPage(ctx, who, filter.Edition, func(t *Tx, shell ui.ShellModel) error {
		var err error
		out.Stands, err = t.scheduleChoices(shell)
		if err != nil {
			return err
		}
		ids := []string{}
		editable := map[string]bool{}
		for _, p := range out.Stands {
			if p.CanEdit || shell.Global {
				if filter.Stand == "" || filter.Stand == "all" || filter.Stand == p.ID {
					ids = append(ids, p.ID)
					editable[p.ID] = p.CanEdit
				}
			}
		}
		if len(ids) == 0 {
			if !shell.Organizer {
				return ErrDenied
			}
			return nil
		}
		if filter.Stand != "" && filter.Stand != "all" {
			found := false
			for _, id := range ids {
				found = found || id == filter.Stand
			}
			if !found {
				return ErrDenied
			}
		}
		q := `SELECT r.*,w.first_name||' '||w.last_name AS name,s.title,s.starts_at,s.ends_at,s.version AS current_version,s.capacity,p.enabled,p.id AS participation_id,e.id AS edition_id,e.label AS edition,st.name AS stand,coalesce(a.confirmed,0) AS confirmed FROM requests r JOIN workers w ON w.id=r.worker_id JOIN shifts s ON s.id=r.shift_id JOIN participations p ON p.id=s.participation_id JOIN editions e ON e.id=p.edition_id JOIN stands st ON st.id=p.stand_id LEFT JOIN (SELECT shift_id,count(*) AS confirmed FROM assignments WHERE active GROUP BY shift_id) a ON a.shift_id=s.id WHERE r.status='pending' AND p.id IN ?`
		args := []any{ids}
		if filter.Day != "" {
			start, err := time.ParseInLocation("2006-01-02", filter.Day, Berlin)
			if err != nil {
				return ErrInvalid
			}
			q += " AND s.starts_at<? AND s.ends_at>?"
			args = append(args, start.AddDate(0, 0, 1), start)
		}
		q += " ORDER BY s.starts_at,r.created_at,r.id LIMIT 21 OFFSET ?"
		args = append(args, (out.Filter.Page-1)*20)
		if err = t.db.Raw(q, args...).Scan(&out.Rows).Error; err != nil {
			return err
		}
		if len(out.Rows) == 0 && out.Filter.Page > 1 {
			page, err := t.remainingRequestPage(q, args, 1)
			if err != nil {
				return err
			}
			out.Filter.Page = page
			args[len(args)-1] = (page - 1) * 20
			if err = t.db.Raw(q, args...).Scan(&out.Rows).Error; err != nil {
				return err
			}
		}
		if len(out.Rows) > 20 {
			out.HasNext = true
			out.Rows = out.Rows[:20]
		}
		for i := range out.Rows {
			out.Rows[i].CanEdit = editable[out.Rows[i].ParticipationID]
			out.Rows[i].Stale = out.Rows[i].ShiftVersion != out.Rows[i].CurrentVersion
		}
		return nil
	})
	out.Shell = shell
	if shell.Global {
		out.Filter.Edition = shell.Selected
	}
	q := filterValues(out.Filter)
	q.Del("page")
	out.ReturnURL = canonical("/requests/pending", q)
	if out.Filter.Page > 1 {
		q.Set("page", n(out.Filter.Page-1))
		out.Previous = canonical("/requests/pending", q)
	}
	if out.HasNext {
		q.Set("page", n(out.Filter.Page+1))
		out.Next = canonical("/requests/pending", q)
	}
	return out, err
}
func (s *Store) requestResource(ctx context.Context, who Identity, id, edition string) (RequestRow, ui.ShellModel, error) {
	var row RequestRow
	shell, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		if err := t.db.Raw("SELECT r.*,s.title,s.starts_at,s.ends_at,p.edition_id,p.id AS participation_id FROM requests r JOIN shifts s ON s.id=r.shift_id JOIN participations p ON p.id=s.participation_id WHERE r.id=?", id).Scan(&row).Error; err != nil {
			return err
		}
		if row.ID == "" {
			return ErrNotFound
		}
		p, err := t.participation(row.ParticipationID)
		if err != nil {
			return err
		}
		if row.WorkerID == t.worker {
			return nil
		}
		err = t.permitPart(p, "edit_schedule")
		if errors.Is(err, ErrDenied) {
			return ErrDenied
		}
		return err
	})
	return row, shell, err
}
func requestsReturn(m RequestsModel) string {
	q := filterValues(m.Filter)
	if m.Status != "" {
		q.Set("status", m.Status)
	}
	path := "/requests/pending"
	if m.ReturnURL != "" {
		if u, err := url.Parse(m.ReturnURL); err == nil {
			path = u.Path
		}
	}
	return canonical(path, q)
}
func requestTone(value string) string {
	switch value {
	case "approved":
		return "success"
	case "pending":
		return "brand"
	case "declined", "cancelled":
		return "warning"
	}
	return "neutral"
}
func requestStatuses() []string {
	return []string{"pending", "approved", "declined", "withdrawn", "cancelled"}
}

// Only an empty page needs an exact count: after the final decision on a page,
// return the remaining final page instead of presenting a misleading empty queue.
func (t *Tx) remainingRequestPage(query string, args []any, orderArgs int) (int, error) {
	base := query[:strings.LastIndex(query, " ORDER BY ")]
	var total int
	if err := t.db.Raw("SELECT count(*) FROM ("+base+") remaining", args[:len(args)-orderArgs]...).Scan(&total).Error; err != nil {
		return 0, err
	}
	return max(1, (total+19)/20), nil
}
