package scheduling

import (
	"context"
	"errors"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
	"net/http"
	"strings"
)

func (h *Handler) returnTo(r *http.Request, raw, fallback string) string {
	if target, ok := parseReturn(raw); ok {
		if nestedRaw := target.Query.Get("return_to"); nestedRaw != "" {
			nested, valid := parseReturn(nestedRaw)
			if !valid || h.store.validateReturn(r.Context(), identity(r), nested) != nil {
				return fallback
			}
		}
		if err := h.store.validateReturn(r.Context(), identity(r), target); err == nil {
			return target.URL()
		}
	}
	return fallback
}
func (s *Store) validateReturn(ctx context.Context, who Identity, target ReturnContext) error {
	_, err := s.readPage(ctx, who, target.Query.Get("edition"), func(t *Tx, _ ui.ShellModel) error {
		if stand := target.Query.Get("member_stand"); stand != "" {
			if e := t.memberPermission(stand); e != nil {
				return e
			}
		}
		if shift := target.Query.Get("shift"); shift != "" {
			_, p, e := t.shift(shift, false)
			if e != nil {
				return e
			}
			if edition := target.Query.Get("edition"); edition != "" && p.EditionID != edition {
				return ErrDenied
			}
			if stand := target.Query.Get("stand"); stand != "" && stand != "all" && p.ID != stand {
				return ErrDenied
			}
			if e = t.permitPart(p, "view_schedule"); e != nil {
				return e
			}
		}
		if stand := target.Query.Get("stand"); stand != "" && stand != "all" {
			p, e := t.participation(stand)
			if e != nil {
				return e
			}
			if target.Query.Get("edition") != "" && p.EditionID != target.Query.Get("edition") {
				return ErrDenied
			}
			if e = t.permitPart(p, "view_schedule"); e != nil {
				return e
			}
		}
		parts := strings.Split(strings.Trim(target.Path, "/"), "/")
		switch parts[0] {
		case "admin":
			if e := t.admin("manage_catalog"); e != nil {
				return e
			}
			if len(parts) > 2 && parts[1] == "editions" && parts[2] != "new" {
				if _, e := t.edition(parts[2]); e != nil {
					return e
				}
				if len(parts) == 5 {
					p, e := t.participation(parts[4])
					if e != nil {
						return e
					}
					if p.EditionID != parts[2] {
						return ErrDenied
					}
				}
			}
			if len(parts) == 3 && parts[1] == "stands" && parts[2] != "new" {
				var exists bool
				if e := t.db.Raw("SELECT EXISTS(SELECT 1 FROM stands WHERE id=?)", parts[2]).Scan(&exists).Error; e != nil {
					return e
				}
				if !exists {
					return ErrNotFound
				}
			}
			return nil
		case "links":
			if len(parts) != 2 {
				return ErrInvalid
			}
			_, e := t.managedCapability(parts[1])
			return e
		case "feedback-notes":
			return t.admin("view_feedback")
		case "directory":
			return t.directoryPermission()
		case "workers":
			if len(parts) < 2 {
				return ErrInvalid
			}
			if len(parts) == 3 && parts[2] == "assign" {
				return t.directoryPermission()
			}
			return t.permitWorker(parts[1], "view_details")
		case "participations":
			if len(parts) < 2 {
				return ErrInvalid
			}
			p, e := t.participation(parts[1])
			if e != nil {
				return e
			}
			return t.permitPart(p, "edit_schedule")
		case "shifts":
			if len(parts) < 2 {
				return ErrInvalid
			}
			_, p, e := t.shift(parts[1], false)
			if e != nil {
				return e
			}
			return t.permitPart(p, "view_schedule")
		case "stands":
			if len(parts) < 2 {
				return ErrInvalid
			}
			return t.memberPermission(parts[1])
		case "requests":
			if len(parts) > 1 && parts[1] == "pending" {
				if t.global {
					return t.admin("manage_catalog")
				}
				// The destination itself applies per-participation checks; an actor
				// still needs a current editable participation to return here.
				var ps []Participation
				if e := t.db.Raw("SELECT p.* FROM participations p JOIN teams tm ON tm.participation_id=p.id WHERE tm.account_id=? AND p.edition_id=? AND p.enabled", t.account, t.current).Scan(&ps).Error; e != nil {
					return e
				}
				for _, p := range ps {
					if e := t.permitPart(p, "edit_schedule"); e == nil {
						return nil
					} else if !errors.Is(e, ErrDenied) {
						return e
					}
				}
				return ErrDenied
			}
		}
		return nil
	})
	return err
}
