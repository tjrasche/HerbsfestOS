package scheduling

import (
	"context"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"time"
)

type PrintDay struct {
	Date   string
	Shifts []ShiftView
}
type PrintModel struct {
	Edition       Edition
	Participation Participation
	Days          []PrintDay
	Now           time.Time
	ReturnURL     string
}

func (s *Store) PrintSchedule(ctx context.Context, who Identity, id, edition, dateFilter string) (PrintModel, error) {
	out := PrintModel{Now: s.Now()}
	_, err := s.readPage(ctx, who, edition, func(t *Tx, shell ui.ShellModel) error {
		p, err := t.participation(id)
		if err != nil {
			return err
		}
		if p.EditionID != shell.Selected {
			return ErrDenied
		}
		if err = t.permitPart(p, "view_schedule"); err != nil {
			return err
		}
		out.Participation = p
		out.Edition, err = t.edition(p.EditionID)
		if err != nil {
			return err
		}
		q := "SELECT * FROM shifts WHERE participation_id=? AND NOT cancelled"
		args := []any{id}
		if dateFilter != "" {
			start, err := time.ParseInLocation("2006-01-02", dateFilter, Berlin)
			if err != nil {
				return ErrInvalid
			}
			q += " AND starts_at<? AND ends_at>?"
			args = append(args, start.AddDate(0, 0, 1), start)
		}
		q += " ORDER BY starts_at,id LIMIT 1000"
		var shifts []Shift
		if err = t.db.Raw(q, args...).Scan(&shifts).Error; err != nil {
			return err
		}
		ids := []string{}
		for _, sh := range shifts {
			ids = append(ids, sh.ID)
		}
		type rosterRow struct{ ShiftID, ID, Name string }
		var roster []rosterRow
		if len(ids) > 0 {
			if err = t.db.Raw("SELECT a.shift_id,w.id,w.first_name||' '||w.last_name AS name FROM assignments a JOIN workers w ON w.id=a.worker_id WHERE a.shift_id IN ? AND a.active ORDER BY w.last_name,w.first_name,w.id", ids).Scan(&roster).Error; err != nil {
				return err
			}
		}
		byShift := map[string][]Person{}
		for _, row := range roster {
			byShift[row.ShiftID] = append(byShift[row.ShiftID], Person{ID: row.ID, Name: row.Name})
		}
		for _, sh := range shifts {
			date := sh.StartsAt.In(Berlin).Format("02.01.2006")
			if len(out.Days) == 0 || out.Days[len(out.Days)-1].Date != date {
				out.Days = append(out.Days, PrintDay{Date: date})
			}
			i := len(out.Days) - 1
			out.Days[i].Shifts = append(out.Days[i].Shifts, ShiftView{Shift: sh, Roster: byShift[sh.ID], Free: sh.Capacity - len(byShift[sh.ID])})
		}
		out.ReturnURL = scheduleURL(ScheduleFilter{Edition: edition, Stand: id, Day: dateFilter})
		return nil
	})
	return out, err
}
