package scheduling

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type StandChoice struct {
	Participation
	CanEdit, CanManageMembers bool
	Pending                   int
}
type DayChoice struct{ Value, Label string }
type ShiftSummary struct {
	Shift
	Stand                      string
	Confirmed, Pending         int
	CanEdit, Outside, Selected bool
	Continuation, Continues    bool
	Top, Height, Lane          int
	Href                       string
}
type TimelineHour struct {
	Label string
	Top   int
}
type TimelineColumn struct {
	Label        string
	Start, Lanes int
}
type ScheduleModel struct {
	Shell                  ui.ShellModel
	Edition                Edition
	Filter                 ScheduleFilter
	Stands                 []StandChoice
	Days                   []DayChoice
	Shifts                 []ShiftSummary
	Hours                  []TimelineHour
	Columns                []TimelineColumn
	Height, Lanes, Pending int
	HasNext                bool
	Previous, Next         string
}

func (m ScheduleModel) URL() string { return scheduleURL(m.Filter) }
func (m ScheduleModel) SelectedStand() StandChoice {
	for _, p := range m.Stands {
		if p.ID == m.Filter.Stand {
			return p
		}
	}
	return StandChoice{}
}
func scheduleURL(f ScheduleFilter) string {
	q := url.Values{}
	if f.Edition != "" {
		q.Set("edition", f.Edition)
	}
	if f.Stand != "" {
		q.Set("stand", f.Stand)
	}
	if f.Day != "" {
		q.Set("day", f.Day)
	}
	if f.Page > 1 {
		q.Set("page", n(f.Page))
	}
	return canonical("/", q)
}
func (s *Store) ScheduleSummary(ctx context.Context, who Identity, filter ScheduleFilter) (ScheduleModel, error) {
	var out ScheduleModel
	shell, err := s.readPage(ctx, who, filter.Edition, func(t *Tx, shell ui.ShellModel) error {
		var err error
		out, err = t.scheduleSummary(shell, filter)
		return err
	})
	out.Shell = shell
	return out, err
}
func (t *Tx) scheduleChoices(shell ui.ShellModel) ([]StandChoice, error) {
	var ps []Participation
	var out []StandChoice
	if shell.Selected == "" {
		return out, nil
	}
	if err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id WHERE p.edition_id=? ORDER BY s.name,p.id", shell.Selected).Scan(&ps).Error; err != nil {
		return nil, err
	}
	for _, p := range ps {
		err := t.permitPart(p, "view_schedule")
		if errors.Is(err, ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		choice := StandChoice{Participation: p}
		err = t.permitPart(p, "edit_schedule")
		choice.CanEdit = err == nil
		if err != nil && !errors.Is(err, ErrDenied) {
			return nil, err
		}
		err = t.memberPermission(p.StandID)
		choice.CanManageMembers = err == nil
		if err != nil && !errors.Is(err, ErrDenied) {
			return nil, err
		}
		out = append(out, choice)
	}
	return out, nil
}
func (t *Tx) scheduleSummary(shell ui.ShellModel, filter ScheduleFilter) (ScheduleModel, error) {
	out := ScheduleModel{Shell: shell, Filter: filter, Lanes: 1}
	if filter.Page < 1 {
		out.Filter.Page = 1
	}
	out.Filter.Edition = ""
	if shell.Global {
		out.Filter.Edition = shell.Selected
	}
	if shell.Selected == "" {
		return out, nil
	}
	var err error
	out.Edition, err = t.edition(shell.Selected)
	if err != nil {
		return out, err
	}
	out.Stands, err = t.scheduleChoices(shell)
	if err != nil {
		return out, err
	}
	if len(out.Stands) == 0 {
		return out, nil
	}
	if out.Filter.Stand == "" {
		out.Filter.Stand = out.Stands[0].ID
	}
	ids := []string{}
	allowed := false
	for _, p := range out.Stands {
		if out.Filter.Stand == "all" || p.ID == out.Filter.Stand {
			ids = append(ids, p.ID)
			allowed = true
		}
	}
	if !allowed {
		return out, ErrDenied
	}
	if err = t.db.Raw(`SELECT DISTINCT to_char(d,'YYYY-MM-DD') AS value,to_char(d,'DD.MM.YYYY') AS label FROM shifts sh CROSS JOIN LATERAL generate_series(date_trunc('day',sh.starts_at AT TIME ZONE 'Europe/Berlin'),date_trunc('day',(sh.ends_at-interval '1 microsecond') AT TIME ZONE 'Europe/Berlin'),interval '1 day') d WHERE sh.participation_id IN ? ORDER BY value LIMIT 62`, ids).Scan(&out.Days).Error; err != nil {
		return out, err
	}
	if out.Filter.Day == "" {
		if len(out.Days) > 0 {
			out.Filter.Day = out.Days[0].Value
		} else {
			out.Filter.Day = date(out.Edition.StartDay)
		}
	}
	dayStart, err := time.ParseInLocation("2006-01-02", out.Filter.Day, Berlin)
	if err != nil {
		return out, ErrInvalid
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	if dayStart.Before(day(out.Edition.StartDay).AddDate(0, 0, -14)) || dayStart.After(day(out.Edition.EndDay).AddDate(0, 0, 14)) {
		return out, ErrInvalid
	}
	q := `WITH chosen AS (SELECT s.* FROM shifts s WHERE participation_id IN ? AND starts_at<? AND ends_at>? ORDER BY starts_at,id LIMIT 101 OFFSET ?), assigned AS (SELECT a.shift_id,count(*) AS confirmed FROM assignments a JOIN chosen c ON c.id=a.shift_id WHERE a.active GROUP BY a.shift_id), pending AS (SELECT r.shift_id,count(*) AS pending FROM requests r JOIN chosen c ON c.id=r.shift_id WHERE r.status='pending' GROUP BY r.shift_id) SELECT c.*,st.name AS stand,COALESCE(a.confirmed,0) AS confirmed,COALESCE(r.pending,0) AS pending FROM chosen c JOIN participations p ON p.id=c.participation_id JOIN stands st ON st.id=p.stand_id LEFT JOIN assigned a ON a.shift_id=c.id LEFT JOIN pending r ON r.shift_id=c.id ORDER BY c.starts_at,c.id`
	if err = t.db.Raw(q, ids, dayEnd, dayStart, (out.Filter.Page-1)*100).Scan(&out.Shifts).Error; err != nil {
		return out, err
	}
	if len(out.Shifts) > 100 {
		out.HasNext = true
		out.Shifts = out.Shifts[:100]
	}
	permissions := map[string]bool{}
	for _, p := range out.Stands {
		permissions[p.ID] = p.CanEdit
	}
	for i := range out.Shifts {
		sh := &out.Shifts[i]
		sh.CanEdit = permissions[sh.ParticipationID]
		sh.Outside = !sh.Within(out.Edition)
		if !sh.CanEdit && !shell.Global {
			sh.Pending = 0
		}
		out.Pending += sh.Pending
		sh.Href = canonical("/shifts/"+sh.ID, filterValues(out.Filter))
	}
	out.computeTimeline(dayStart, dayEnd)
	if out.Filter.Page > 1 {
		f := out.Filter
		f.Page--
		out.Previous = scheduleURL(f)
	}
	if out.HasNext {
		f := out.Filter
		f.Page++
		out.Next = scheduleURL(f)
	}
	return out, nil
}
func filterValues(f ScheduleFilter) url.Values {
	q := url.Values{}
	if f.Edition != "" {
		q.Set("edition", f.Edition)
	}
	if f.Stand != "" {
		q.Set("stand", f.Stand)
	}
	if f.Day != "" {
		q.Set("day", f.Day)
	}
	if f.Page > 1 {
		q.Set("page", n(f.Page))
	}
	return q
}
func (m *ScheduleModel) computeTimeline(start, end time.Time) {
	minMinute, maxMinute := 24*60, 0
	for _, sh := range m.Shifts {
		a, b := max(sh.StartsAt.Unix(), start.Unix()), min(sh.EndsAt.Unix(), end.Unix())
		begin, finish := int((a-start.Unix())/60), int((b-start.Unix())/60)
		minMinute = min(minMinute, begin)
		maxMinute = max(maxMinute, finish)
	}
	if len(m.Shifts) == 0 {
		minMinute = 8 * 60
		maxMinute = 12 * 60
	}
	minMinute = max(0, (minMinute/60)*60-30)
	maxMinute = min(int(end.Sub(start).Minutes()), ((maxMinute+59)/60)*60+30)
	m.Height = max(240, maxMinute-minMinute)
	lanes := map[string][]time.Time{}
	for i := range m.Shifts {
		sh := &m.Shifts[i]
		displayStart := sh.StartsAt
		if displayStart.Before(start) {
			displayStart = start
		}
		sh.Top = int(max(sh.StartsAt.Unix(), start.Unix())-start.Unix())/60 - minMinute
		sh.Height = max(70, int(min(sh.EndsAt.Unix(), end.Unix())-max(sh.StartsAt.Unix(), start.Unix()))/60)
		displayEnd := displayStart.Add(time.Duration(sh.Height) * time.Minute)
		m.Height = max(m.Height, sh.Top+sh.Height)
		partLanes := lanes[sh.ParticipationID]
		lane := 0
		for lane < len(partLanes) && partLanes[lane].After(displayStart) {
			lane++
		}
		if lane == len(partLanes) {
			partLanes = append(partLanes, displayEnd)
		} else {
			partLanes[lane] = displayEnd
		}
		lanes[sh.ParticipationID] = partLanes
		sh.Lane = lane
		sh.Continuation = sh.StartsAt.Before(start)
		sh.Continues = sh.EndsAt.After(end)
	}
	offsets := map[string]int{}
	m.Lanes = 0
	for _, p := range m.Stands {
		if m.Filter.Stand != "all" && m.Filter.Stand != p.ID {
			continue
		}
		width := max(1, len(lanes[p.ID]))
		offsets[p.ID] = m.Lanes
		m.Columns = append(m.Columns, TimelineColumn{Label: p.Name, Start: m.Lanes, Lanes: width})
		m.Lanes += width
	}
	for _, sh := range m.Shifts {
		if _, ok := offsets[sh.ParticipationID]; !ok {
			offsets[sh.ParticipationID] = m.Lanes
			width := max(1, len(lanes[sh.ParticipationID]))
			m.Columns = append(m.Columns, TimelineColumn{Label: nonempty(sh.Stand, "Dienste"), Start: m.Lanes, Lanes: width})
			m.Lanes += width
		}
	}
	m.Lanes = max(1, m.Lanes)
	for i := range m.Shifts {
		m.Shifts[i].Lane += offsets[m.Shifts[i].ParticipationID]
	}
	for minute := ((minMinute + 59) / 60) * 60; minute <= maxMinute; minute += 60 {
		m.Hours = append(m.Hours, TimelineHour{Label: start.Add(time.Duration(minute) * time.Minute).Format("15:04"), Top: minute - minMinute})
	}
}
func timelineStyle(m ScheduleModel) string {
	return fmt.Sprintf("--timeline-height:%dpx;--timeline-lanes:%d", m.Height, m.Lanes)
}
func shiftStyle(sh ShiftSummary) string {
	return fmt.Sprintf("--shift-top:%dpx;--shift-height:%dpx;--shift-lane:%d", sh.Top, sh.Height, sh.Lane)
}
func conciseTime(s Shift) string {
	end := s.EndsAt.In(Berlin).Format("15:04")
	if s.StartsAt.In(Berlin).Format("2006-01-02") != s.EndsAt.In(Berlin).Format("2006-01-02") {
		end = s.EndsAt.In(Berlin).Format("02.01. 15:04")
	}
	return s.StartsAt.In(Berlin).Format("02.01. 15:04") + "–" + end
}
func dayLabel(value string) string {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return t.Format("02.01.2006")
}
func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

type ShiftDetailModel struct {
	Shell                                 ui.ShellModel
	Schedule                              ScheduleModel
	Participation                         Participation
	Edition                               Edition
	Shift                                 ShiftView
	CanEdit, CanManageMembers, CanRequest bool
	ReturnURL                             string
}

func (s *Store) ShiftDetail(ctx context.Context, who Identity, id string, filter ScheduleFilter) (ShiftDetailModel, error) {
	var out ShiftDetailModel
	shell, err := s.readPage(ctx, who, filter.Edition, func(t *Tx, shell ui.ShellModel) error {
		sh, p, err := t.shift(id, false)
		if err != nil {
			return err
		}
		if sh.ID == "" {
			return ErrNotFound
		}
		if err = t.permitPart(p, "view_schedule"); err != nil {
			return err
		}
		if p.EditionID != shell.Selected {
			return ErrDenied
		}
		out.Participation = p
		out.Edition, err = t.edition(p.EditionID)
		if err != nil {
			return err
		}
		out.Shift = ShiftView{Shift: sh, Outside: !sh.Within(out.Edition)}
		err = t.permitPart(p, "edit_schedule")
		out.CanEdit = err == nil
		if err != nil && !errors.Is(err, ErrDenied) {
			return err
		}
		err = t.memberPermission(p.StandID)
		out.CanManageMembers = err == nil
		if err != nil && !errors.Is(err, ErrDenied) {
			return err
		}
		if err = t.db.Raw("SELECT w.id,w.first_name||' '||w.last_name AS name FROM assignments a JOIN workers w ON w.id=a.worker_id WHERE a.shift_id=? AND a.active ORDER BY w.last_name,w.first_name,w.id", id).Scan(&out.Shift.Roster).Error; err != nil {
			return err
		}
		out.Shift.Free = sh.Capacity - len(out.Shift.Roster)
		if out.CanEdit || t.global {
			if err = t.db.Raw("SELECT r.*,w.first_name||' '||w.last_name AS name FROM requests r JOIN workers w ON w.id=r.worker_id WHERE r.shift_id=? AND r.status='pending' ORDER BY r.created_at,r.id LIMIT 10", id).Scan(&out.Shift.Pending).Error; err != nil {
				return err
			}
			for i := range out.Shift.Pending {
				out.Shift.Pending[i].Stale = out.Shift.Pending[i].ShiftVersion != sh.Version
			}
			if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM assignments a JOIN assignments b ON b.worker_id=a.worker_id JOIN shifts other ON other.id=b.shift_id WHERE a.shift_id=? AND a.active AND b.active AND b.shift_id<>? AND NOT other.cancelled AND other.starts_at<? AND other.ends_at>?)", id, id, sh.EndsAt, sh.StartsAt).Scan(&out.Shift.Overlap).Error; err != nil {
				return err
			}
		}
		if t.worker != "" && p.Enabled && p.EditionID == t.current && !sh.Cancelled && sh.StartsAt.After(t.store.Now()) && out.Shift.Free > 0 {
			var member bool
			if err = t.db.Raw("SELECT EXISTS(SELECT 1 FROM memberships WHERE stand_id=? AND worker_id=?)", p.StandID, t.worker).Scan(&member).Error; err != nil {
				return err
			}
			out.CanRequest = member
			if member {
				err = t.signupPermit(sh, p, "request_place")
				if err != nil && !errors.Is(err, ErrDenied) {
					return err
				}
				out.CanRequest = err == nil
			}
		}
		filter.Stand = p.ID
		if filter.Day == "" {
			filter.Day = sh.StartsAt.In(Berlin).Format("2006-01-02")
		}
		out.Schedule, err = t.scheduleSummary(shell, filter)
		if err != nil {
			return err
		}
		for i := range out.Schedule.Shifts {
			out.Schedule.Shifts[i].Selected = out.Schedule.Shifts[i].ID == sh.ID
		}
		out.ReturnURL = out.Schedule.URL()
		return nil
	})
	out.Shell = shell
	return out, err
}
func shiftCurrent(selected bool) string {
	if selected {
		return "page"
	}
	return "false"
}
func cancelledLabel(cancelled bool) string {
	if cancelled {
		return ", abgesagt"
	}
	return ""
}
func assignmentQuery(m ShiftDetailModel) url.Values {
	q := filterValues(m.Schedule.Filter)
	q.Set("shift", m.Shift.ID)
	q.Set("return_to", canonical("/shifts/"+m.Shift.ID, filterValues(m.Schedule.Filter)))
	return q
}
