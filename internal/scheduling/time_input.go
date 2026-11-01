package scheduling

import (
	"fmt"
	"time"
)

// berlinMoment accepts ordinary wall-clock input without asking the user to
// choose an offset. Only the autumn repeated hour requires an explicit choice.
func berlinMoment(value, offset string) (string, bool, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format(time.RFC3339), false, nil
	}
	wall, err := time.Parse("2006-01-02T15:04", value)
	if err != nil {
		return "", false, fmt.Errorf("%w Beginn und Ende als Datum und Uhrzeit angeben.", ErrInvalid)
	}
	var candidates []time.Time
	for _, seconds := range []int{3600, 7200} {
		candidate := wall.Add(-time.Duration(seconds) * time.Second).In(Berlin)
		if candidate.Format("2006-01-02T15:04") == value {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return "", false, fmt.Errorf("%w Diese Berliner Ortszeit existiert wegen der Zeitumstellung nicht.", ErrInvalid)
	}
	if len(candidates) == 1 {
		return candidates[0].Format(time.RFC3339), false, nil
	}
	for _, candidate := range candidates {
		if candidate.Format("-07:00") == offset {
			return candidate.Format(time.RFC3339), true, nil
		}
	}
	return "", true, fmt.Errorf("%w Diese Uhrzeit kommt bei der Zeitumstellung zweimal vor. Bitte die erste oder zweite Stunde wählen.", ErrInvalid)
}
