package scheduling

import (
	"errors"
	"testing"
	"time"
)

func TestBerlinMomentRequiresChoiceOnlyForRepeatedHour(t *testing.T) {
	for _, tc := range []struct {
		input, offset, want string
		ambiguous, invalid  bool
	}{
		{"2027-09-10T09:00", "", "2027-09-10T09:00:00+02:00", false, false},
		{"2027-01-10T09:00", "", "2027-01-10T09:00:00+01:00", false, false},
		{"2027-03-28T02:30", "", "", false, true},
		{"2027-10-31T02:30", "", "", true, true},
		{"2027-10-31T02:30", "+02:00", "2027-10-31T02:30:00+02:00", true, false},
		{"2027-10-31T02:30", "+01:00", "2027-10-31T02:30:00+01:00", true, false},
	} {
		t.Run(tc.input+tc.offset, func(t *testing.T) {
			got, ambiguous, err := berlinMoment(tc.input, tc.offset)
			if (err != nil) != tc.invalid || ambiguous != tc.ambiguous || got != tc.want {
				t.Fatalf("got %q ambiguous=%t err=%v", got, ambiguous, err)
			}
			if err != nil && !errors.Is(err, ErrInvalid) {
				t.Fatal("lost validation status", err)
			}
		})
	}
}
func TestTimelineClipsOvernightWithoutChangingStoredInterval(t *testing.T) {
	start := time.Date(2027, 9, 10, 0, 0, 0, 0, Berlin)
	before := start.Add(-time.Hour)
	after := start.Add(2 * time.Hour)
	m := ScheduleModel{Shifts: []ShiftSummary{{Shift: Shift{StartsAt: before, EndsAt: after}}}}
	m.computeTimeline(start, start.AddDate(0, 0, 1))
	if m.Shifts[0].Top != 0 || m.Shifts[0].Height != 120 || !m.Shifts[0].StartsAt.Equal(before) || !m.Shifts[0].EndsAt.Equal(after) {
		t.Fatalf("overnight segment changed: %+v", m.Shifts[0])
	}
}

func TestTimelineAdjacentShortShiftsUseSeparatePresentationLanes(t *testing.T) {
	start := time.Date(2027, 9, 10, 0, 0, 0, 0, Berlin)
	first := start.Add(9 * time.Hour)
	m := ScheduleModel{Shifts: []ShiftSummary{{Shift: Shift{ParticipationID: "stand", StartsAt: first, EndsAt: first.Add(30 * time.Minute)}}, {Shift: Shift{ParticipationID: "stand", StartsAt: first.Add(30 * time.Minute), EndsAt: first.Add(time.Hour)}}}}
	m.computeTimeline(start, start.AddDate(0, 0, 1))
	a, b := m.Shifts[0], m.Shifts[1]
	if a.Lane == b.Lane && a.Top+a.Height > b.Top {
		t.Fatalf("adjacent short blocks overlap in lane %d: first=%+v second=%+v", a.Lane, a, b)
	}
	if b.Top-a.Top != 30 {
		t.Fatal("start times lost proportional position")
	}
}
