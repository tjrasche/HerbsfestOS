package scheduling

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrDenied      = errors.New("Für diese Aktion fehlt die Berechtigung.")
	ErrUnavailable = errors.New("Berechtigungen sind derzeit nicht erreichbar. Bitte erneut versuchen.")
	ErrInvalid     = errors.New("Bitte Eingaben und Auswahl prüfen.")
	ErrConflict    = errors.New("Daten wurden geändert oder der Dienst ist voll. Bitte Auswahl prüfen und erneut versuchen.")
	Berlin         = mustBerlin()
)

func mustBerlin() *time.Location {
	v, e := time.LoadLocation("Europe/Berlin")
	if e != nil {
		panic(e)
	}
	return v
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func textValid(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}

type Identity struct{ Issuer, Subject string }
type Edition struct {
	ID, Label        string
	StartDay, EndDay time.Time
}
type Participation struct {
	ID, EditionID, StandID, Name string
	Enabled                      bool
}
type Worker struct {
	ID, FirstName, LastName, Email, Phone, Notes string
	AccountID                                    *string
}

func (w Worker) Name() string { return w.FirstName + " " + w.LastName }

type Shift struct {
	ID, ParticipationID, Title string
	StartsAt, EndsAt           time.Time
	Capacity, Version          int
	Cancelled                  bool
}

func (s Shift) Time() string {
	return s.StartsAt.In(Berlin).Format("02.01.2006 15:04 MST -0700") + " – " + s.EndsAt.In(Berlin).Format("02.01.2006 15:04 MST -0700")
}
func (s Shift) Within(e Edition) bool {
	a := day(e.StartDay).AddDate(0, 0, -14)
	b := day(e.EndDay).AddDate(0, 0, 15)
	return s.EndsAt.After(s.StartsAt) && !s.StartsAt.Before(a) && !s.EndsAt.After(b)
}
func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Berlin) }

type Request struct {
	ID, ShiftID, WorkerID, Status string
	ShiftVersion                  int
	CreatedAt                     time.Time
}
type Capability struct {
	Digest, Kind                                 string
	ParticipationID, ShiftID, WorkerID, IssuerID *string
	Scope                                        string
	ExpiresAt                                    *time.Time
	Disabled                                     bool
	ConsumedBy                                   *string
	CreatedAt                                    time.Time
}
type Tuple struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}
type Checker interface {
	CheckContext(ctx context.Context, user, relation, object string, facts []Tuple) (bool, error)
}
