package scheduling

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

type FormState = ui.FormState
type ScheduleFilter struct {
	Edition, Stand, Day string
	Page                int
}

// readPage constructs shell and feature data under one trusted-fact snapshot.
// Feature reads must use the supplied Tx, and must never nest Store methods.
func (s *Store) readPage(ctx context.Context, who Identity, edition string, read func(*Tx, ui.ShellModel) error) (ui.ShellModel, error) {
	var shell ui.ShellModel
	err := s.transact(ctx, who, false, func(t *Tx) error {
		var err error
		shell, err = t.shell(edition)
		if err != nil {
			return err
		}
		if read != nil {
			return read(t, shell)
		}
		return nil
	})
	return shell, err
}
func (s *Store) Shell(ctx context.Context, who Identity, edition string) (ui.ShellModel, error) {
	return s.readPage(ctx, who, edition, nil)
}
func (t *Tx) shell(edition string) (ui.ShellModel, error) {
	v := ui.ShellModel{Private: true, Global: t.global, AccountID: t.account, WorkerID: t.worker, Current: t.current, Selected: t.current}
	t.resetFacts()
	t.fact(t.principal(), "owner", "worker:availability")
	if err := t.check("view_details", "worker:availability"); err != nil {
		return v, err
	}
	if edition != "" && edition != t.current {
		if err := t.admin("select_edition"); err != nil {
			return v, err
		}
		v.Selected = edition
	}
	if v.Current != "" {
		e, err := t.edition(v.Current)
		if err != nil {
			return v, err
		}
		v.CurrentEdition = e.Label
	}
	if v.Selected != "" {
		if v.Selected == v.Current {
			v.SelectedEdition = v.CurrentEdition
		} else {
			e, err := t.edition(v.Selected)
			if err != nil {
				return v, err
			}
			v.SelectedEdition = e.Label
		}
	}
	if t.global {
		if err := t.admin("manage_catalog"); err != nil {
			return v, err
		}
		if err := t.db.Raw("SELECT id,label FROM editions ORDER BY start_day DESC,label,id LIMIT 100").Scan(&v.Editions).Error; err != nil {
			return v, err
		}
		v.Organizer = true
	} else if t.account != "" && t.current != "" {
		var ps []Participation
		if err := t.db.Raw("SELECT p.*,s.name FROM participations p JOIN stands s ON s.id=p.stand_id JOIN teams tm ON tm.participation_id=p.id WHERE tm.account_id=? AND p.edition_id=? AND p.enabled ORDER BY p.id", t.account, t.current).Scan(&ps).Error; err != nil {
			return v, err
		}
		for _, p := range ps {
			err := t.permitPart(p, "edit_schedule")
			if err == nil {
				v.Organizer = true
				break
			}
			if !errors.Is(err, ErrDenied) {
				return v, err
			}
		}
	}
	return v, nil
}
func requestEdition(r *http.Request) string {
	if edition := r.PathValue("edition"); edition != "" {
		return edition
	}
	if edition := r.URL.Query().Get("edition"); edition != "" {
		return edition
	}
	return ""
}
func pageNumber(r *http.Request) int { return boundedPage(r.URL.Query().Get("page")) }
func formState(r *http.Request, err error) FormState {
	v := FormState{ID: "form-errors", Values: r.PostForm, Errors: map[string]string{}}
	if err != nil {
		v.Summary = errorMessage(err)
	}
	return v
}
func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}
func canonical(path string, values url.Values) string {
	if q := values.Encode(); q != "" {
		return path + "?" + q
	}
	return path
}
func contextQuery(shell ui.ShellModel) url.Values {
	v := url.Values{}
	if shell.Global && shell.Selected != "" {
		v.Set("edition", shell.Selected)
	}
	return v
}
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
}
func shortID(id string) string {
	if len(id) > 8 {
		return id[len(id)-8:]
	}
	return id
}
func nonempty(values ...string) string {
	for _, s := range values {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
